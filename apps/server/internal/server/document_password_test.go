package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_DocumentPasswordCreatorAndUnlockGeneration(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, ownerToken := registerAccessUser(t, serve, "lock-space-owner@example.test")
	creatorID, _, creatorToken := registerAccessUser(t, serve, "lock-creator@example.test")
	_, _, visitorToken := registerAccessUser(t, serve, "lock-visitor@example.test")
	const spaceID = "lock-space"
	const documentID = "lock-document"
	const base = "/api/docs/" + documentID
	seedSpaceAndDocumentForAccess(t, database, ownerID, spaceID, "lock-node", documentID, "public", "public")
	seedSpaceMemberForAccess(t, database, spaceID, creatorID, "collaborator")
	if err := database.ORM.Model(&models.Document{}).Where(models.DocumentColumns.DocumentID+" = ?", documentID).
		Update(models.DocumentColumns.CreatedByUserID, creatorID).Error; err != nil {
		t.Fatal(err)
	}

	call := func(method, target, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if cookie != nil {
			req.AddCookie(cookie)
		}
		return serve(req)
	}
	requireCode := func(result *httptest.ResponseRecorder, expected int) {
		t.Helper()
		if code := decodeJSONResultCode(t, result.Body.Bytes()); code != expected {
			t.Fatalf("expected code %d, got %d (%s)", expected, code, result.Body.String())
		}
	}
	requireLocked := func(result *httptest.ResponseRecorder) {
		t.Helper()
		requireCode(result, response.ResolveErrorCode(response.CodeDocumentPasswordRequired))
		if strings.Contains(result.Body.String(), "# hello") {
			t.Fatal("a locked response leaked the document")
		}
	}
	requireCode(call(http.MethodPut, base+"/lock", ownerToken, `{"password":"example-pass"}`, nil),
		response.ResolveErrorCode(response.CodeForbidden))
	requireCode(call(http.MethodPut, base+"/lock", creatorToken, `{"password":"example-pass"}`, nil), 0)
	requireCode(call(http.MethodPut, base+"/lock", creatorToken, `{}`, nil), response.ResolveErrorCode(response.CodeInvalidRequest))

	for _, target := range []string{base, base + "/revisions", base + "/attachments", base + "/onlyoffice/edit-config"} {
		requireLocked(call(http.MethodGet, target, creatorToken, "", nil))
	}
	requireLocked(call(http.MethodGet, base, "", "", nil))
	requireCode(call(http.MethodPost, base+"/unlock", visitorToken, `{"password":"incorrect"}`, nil),
		response.ResolveErrorCode(response.CodeDocumentPasswordInvalid))

	result := call(http.MethodPost, base+"/unlock", visitorToken, `{"password":"example-pass"}`, nil)
	requireCode(result, 0)
	var grant *http.Cookie
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == service.DocumentUnlockCookieName(documentID) {
			grant = cookie
		}
	}
	if grant == nil || !grant.HttpOnly || grant.Value == "" {
		t.Fatal("expected an HTTP-only document grant")
	}
	unlocked := call(http.MethodGet, base, visitorToken, "", grant)
	requireCode(unlocked, 0)
	if !strings.Contains(unlocked.Body.String(), "# hello") || !strings.Contains(unlocked.Header().Get("Cache-Control"), "no-store") {
		t.Fatal("unlocked document did not return its body with anti-cache headers")
	}
	requireLocked(call(http.MethodGet, base, ownerToken, "", grant))
	requireCode(call(http.MethodPut, base+"/lock", creatorToken, `{"password":"rotated-pass"}`, nil), 0)
	requireLocked(call(http.MethodGet, base, visitorToken, "", grant))

	requireCode(call(http.MethodPut, base+"/lock", creatorToken, `{"password":null}`, nil), 0)
	requireCode(call(http.MethodGet, base, "", "", nil), 0)
}

func TestRouter_DocumentPasswordInvalidatesOnlyOfficeSourceLinks(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, ownerToken := registerAccessUser(t, serve, "lock-office-owner@example.test")
	const spaceID = "lock-office-space"
	seedOnlyOfficeEnabledConfig(t, database)
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")
	documentID := createOfficeDocumentForOnlyOfficeTest(t, serve, spaceID, ownerToken, "docx", "文档锁合同")
	publishOnlyOfficeReaderDocument(t, database, spaceID, documentID)
	base := "/api/docs/" + documentID
	viewConfigURL := "/api/reader/spaces/" + spaceID + "/docs/" + documentID + "/onlyoffice/view-config"

	call := func(method, target, token, body string, cookie *http.Cookie) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, target, strings.NewReader(body))
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		if body != "" {
			request.Header.Set("Content-Type", "application/json")
		}
		if cookie != nil {
			request.AddCookie(cookie)
		}
		return serve(request)
	}
	type configuration struct {
		Config struct {
			Document struct{ URL, Key string } `json:"document"`
		} `json:"config"`
	}
	viewConfig := func(cookie *http.Cookie) configuration {
		t.Helper()
		result := call(http.MethodGet, viewConfigURL, "", "", cookie)
		if code := decodeJSONResultCode(t, result.Body.Bytes()); code != 0 {
			t.Fatalf("cannot read OnlyOffice configuration: %s", result.Body.String())
		}
		config := decodeJSONResultData[configuration](t, result.Body.Bytes())
		if config.Config.Document.URL == "" || config.Config.Document.Key == "" {
			t.Fatal("missing OnlyOffice source configuration")
		}
		return config
	}
	setPassword := func(body string) {
		t.Helper()
		result := call(http.MethodPut, base+"/lock", ownerToken, body, nil)
		if code := decodeJSONResultCode(t, result.Body.Bytes()); code != 0 {
			t.Fatalf("cannot set document password: %s", result.Body.String())
		}
	}

	previous := viewConfig(nil)
	setPassword(`{"password":"office-first"}`)
	if result := call(http.MethodGet, previous.Config.Document.URL, "", "", nil); result.Code != http.StatusForbidden {
		t.Fatalf("unlocked configuration source link returned HTTP %d after setting the password", result.Code)
	}
	unlock := call(http.MethodPost, base+"/unlock", "", `{"password":"office-first"}`, nil)
	cookies := unlock.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != service.DocumentUnlockCookieName(documentID) {
		t.Fatalf("missing document unlock cookie: %s", unlock.Body.String())
	}
	current := viewConfig(cookies[0])
	if current.Config.Document.Key == previous.Config.Document.Key {
		t.Fatal("OnlyOffice reused the key from the unlocked configuration")
	}
	if result := call(http.MethodGet, current.Config.Document.URL, "", "", nil); result.Code != http.StatusOK {
		t.Fatalf("OnlyOffice source could not be retrieved using its signed token (HTTP %d)", result.Code)
	}
	setPassword(`{"password":"office-rotated"}`)
	if result := call(http.MethodGet, current.Config.Document.URL, "", "", nil); result.Code != http.StatusForbidden {
		t.Fatalf("stale locked source link returned HTTP %d after password rotation", result.Code)
	}
}
