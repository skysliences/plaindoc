package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_PlatformAdminReadsPrivateDocumentsAndFolders(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	_, _, ownerToken := registerAccessUser(t, serve, "review-owner@example.com")
	adminID, adminEmail, adminToken := registerAccessUser(t, serve, "review-platform-admin@example.com")
	memberID, memberEmail, memberToken := registerAccessUser(t, serve, "review-space-admin@example.com")
	_, _, outsiderToken := registerAccessUser(t, serve, "review-outsider@example.com")

	for _, role := range []models.UserAdminRole{
		{UserID: adminID, Role: models.AdminRolePlatformAdmin},
		{UserID: memberID, Role: models.AdminRoleSpaceAdmin},
	} {
		if err := database.ORM.Create(&role).Error; err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := serve(req)
		if rec.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}

	space := decodeJSONResultData[struct {
		ID                string
		ExampleDocumentID string
	}](t, request(http.MethodPost, "/api/spaces", ownerToken, `{"name":"成员可见空间"}`, http.StatusOK).Body.Bytes())
	spacePath := "/api/spaces/" + space.ID
	root := decodeJSONResultData[struct{ NodeID string }](
		t,
		request(http.MethodPost, spacePath+"/nodes", ownerToken, `{"type":"folder","title":"内部文件夹"}`, http.StatusOK).Body.Bytes(),
	)
	nested := decodeJSONResultData[struct{ DocID string }](
		t,
		request(http.MethodPost, spacePath+"/nodes", ownerToken, `{"type":"doc","title":"敏感文档","parentId":"`+root.NodeID+`"}`, http.StatusOK).Body.Bytes(),
	)
	docPath := "/api/docs/" + nested.DocID
	readerPath := "/r/" + space.ID
	exportPath := "/api/reader/spaces/" + space.ID + "/docs/" + nested.DocID + "/export-access"

	request(http.MethodGet, spacePath, adminToken, "", http.StatusOK)
	request(http.MethodGet, spacePath, outsiderToken, "", http.StatusForbidden)
	request(http.MethodGet, docPath, adminToken, "", http.StatusOK)
	request(http.MethodGet, docPath, outsiderToken, "", http.StatusForbidden)
	for _, member := range []struct{ userID, email string }{{adminID, adminEmail}, {memberID, memberEmail}} {
		request(http.MethodPut, spacePath+"/members", ownerToken, `{"email":"`+member.email+`","role":"reader"}`, http.StatusOK)
		request(http.MethodPut, docPath+"/permissions/"+member.userID, ownerToken, `{"access":"denied"}`, http.StatusOK)
	}

	if document := request(http.MethodGet, docPath, adminToken, "", http.StatusOK); !strings.Contains(document.Body.String(), `"copyDenied":true`) {
		t.Fatal("platform review must preserve clipboard restrictions")
	}
	reader := request(http.MethodGet, readerPath+"/"+nested.DocID, adminToken, "", http.StatusOK)
	for _, content := range []string{"内部文件夹", "敏感文档", `"copyDenied":true`, `"exportDenied":true`} {
		if !strings.Contains(reader.Body.String(), content) {
			t.Fatalf("platform admin reader is missing %q", content)
		}
	}
	if strings.Contains(reader.Body.String(), "/"+nested.DocID+"?from=reader") {
		t.Fatal("platform review must not generate a document edit URL")
	}
	request(http.MethodPost, docPath+"/copy-events", adminToken, "", http.StatusForbidden)
	request(http.MethodPost, exportPath, adminToken, "", http.StatusForbidden)
	request(http.MethodPut, docPath, adminToken, `{"contentMd":"不应保存","baseVersion":1}`, http.StatusForbidden)
	request(http.MethodGet, docPath, memberToken, "", http.StatusForbidden)
	request(http.MethodGet, readerPath+"/"+nested.DocID, memberToken, "", http.StatusForbidden)
	memberReader := request(http.MethodGet, readerPath+"/"+space.ExampleDocumentID, memberToken, "", http.StatusOK)
	if strings.Contains(memberReader.Body.String(), nested.DocID) {
		t.Fatal("space administrator must not see a denied document in the reader tree")
	}

	for _, documentID := range []string{nested.DocID, space.ExampleDocumentID} {
		if err := database.ORM.Model(&models.Document{}).
			Where(models.DocumentColumns.DocumentID+" = ?", documentID).
			Update(models.DocumentColumns.Status, models.EntityStatusBanned).Error; err != nil {
			t.Fatal(err)
		}
	}
	request(http.MethodGet, docPath, adminToken, "", http.StatusForbidden)
	directory := request(http.MethodGet, readerPath, adminToken, "", http.StatusOK)
	if body := directory.Body.String(); !strings.Contains(body, "内部文件夹") ||
		!strings.Contains(body, "此空间暂无可阅读文档") || strings.Contains(body, "敏感文档") {
		t.Fatalf("folder-only reader does not show the expected directory: %s", body)
	}
	request(http.MethodGet, readerPath, outsiderToken, "", http.StatusForbidden)
}
