package server

import (
	"bytes"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_ImportedOriginalDownloadUsesPerFilePermissions(t *testing.T) {
	t.Chdir(t.TempDir())
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, ownerToken := registerAccessUser(t, serve, "original-owner@example.com")
	readerID, readerEmail, readerToken := registerAccessUser(t, serve, "original-reader@example.com")
	spaceID := "original-download-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")
	spacePath := "/api/spaces/" + spaceID
	readerPath := "/r/" + spaceID
	request := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
		}
		req.Header.Set("Content-Type", "application/json")
		rec := serve(req)
		if rec.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}

	const originalFileName = "原始资料.pdf"
	originalBytes := []byte("%PDF-1.7\x00\xff\n")
	body, contentType := buildWorkspaceImportMultipartBody(t, "", map[string][]byte{
		originalFileName: originalBytes,
		"备用.pdf":         []byte("%PDF-1.4"),
	})
	importRequest := httptest.NewRequest(http.MethodPost, spacePath+"/imports", body)
	importRequest.Header.Set("Authorization", "Bearer "+ownerToken)
	importRequest.Header.Set("Content-Type", contentType)
	imported := serve(importRequest)
	if imported.Code != http.StatusOK || decodeJSONResultCode(t, imported.Body.Bytes()) != 0 {
		t.Fatalf("could not import original files: %s", imported.Body.String())
	}
	now := time.Now().UTC()
	legacy := models.Node{NodeID: "legacy-unsupported-file", SpaceID: spaceID, Type: models.NodeTypeDoc,
		ImportedFileKind: models.ImportedFileKindUnsupported, Title: "旧文件.pdf", CreatedAt: now, UpdatedAt: now}
	if err := database.ORM.Create(&legacy).Error; err != nil {
		t.Fatal(err)
	}

	tree := decodeJSONResultData[[]struct {
		ID                       string `json:"id"`
		Title                    string `json:"title"`
		DocumentID               string `json:"documentId"`
		DocumentRouteKey         string `json:"documentRouteKey"`
		ImportedOriginalDownload string `json:"importedOriginalDownloadUrl"`
	}](t, request(http.MethodGet, spacePath+"/tree", ownerToken, "", http.StatusOK).Body.Bytes())
	files := make(map[string]string)
	for _, node := range tree {
		if node.ID == legacy.NodeID {
			if node.DocumentID != "" || node.ImportedOriginalDownload != "" {
				t.Fatalf("legacy placeholder has an original download: %+v", node)
			}
			continue
		}
		if node.DocumentID == "" || node.DocumentRouteKey != "" || node.ImportedOriginalDownload != "/api/docs/"+node.DocumentID+"/imported-original" {
			t.Fatalf("imported original has unexpected directory capabilities: %+v", node)
		}
		files[node.Title] = node.DocumentID
	}
	fileID, alternativeID := files[originalFileName], files["备用.pdf"]
	if fileID == "" || alternativeID == "" {
		t.Fatalf("missing imported original files: %+v", files)
	}

	filePath := "/api/docs/" + fileID
	permissionPath := filePath + "/permissions/" + readerID
	downloadPath := filePath + "/imported-original"
	alternativePath := "/api/docs/" + alternativeID + "/imported-original"
	request(http.MethodGet, downloadPath, "", "", http.StatusForbidden)
	request(http.MethodGet, downloadPath, readerToken, "", http.StatusForbidden)
	request(http.MethodGet, "/api/docs/"+legacy.NodeID+"/imported-original", ownerToken, "", http.StatusNotFound)
	if code := decodeJSONResultCode(t, request(http.MethodGet, filePath, ownerToken, "", http.StatusOK).Body.Bytes()); code == 0 {
		t.Fatal("unsupported file unexpectedly opened as a document")
	}
	request(http.MethodGet, readerPath+"/"+fileID, ownerToken, "", http.StatusNotFound)
	request(http.MethodPut, spacePath+"/members", ownerToken, `{"email":"`+readerEmail+`","role":"reader"}`, http.StatusOK)
	request(http.MethodPatch, "/api/nodes/"+fileID, ownerToken, `{"title":"重命名.pdf"}`, http.StatusOK)
	readerHTML := request(http.MethodGet, readerPath, readerToken, "", http.StatusOK).Body.String()
	if !strings.Contains(readerHTML, "重命名.pdf") || !strings.Contains(readerHTML, downloadPath) || !strings.Contains(readerHTML, "旧文件.pdf") {
		t.Fatal("allowed reader cannot find the original or the retained placeholder in the file-only space")
	}
	download := request(http.MethodGet, downloadPath, readerToken, "", http.StatusOK)
	disposition, params, err := mime.ParseMediaType(download.Header().Get("Content-Disposition"))
	if err != nil || disposition != "attachment" || params["filename"] != originalFileName ||
		download.Header().Get("Content-Type") != "application/octet-stream" || !bytes.Equal(download.Body.Bytes(), originalBytes) {
		t.Fatalf("download did not preserve the original bytes and filename: headers %v, body %q", download.Header(), download.Body.Bytes())
	}
	request(http.MethodPut, permissionPath, ownerToken, `{"exportDenied":true}`, http.StatusOK)
	request(http.MethodGet, downloadPath, readerToken, "", http.StatusForbidden)
	request(http.MethodGet, alternativePath, readerToken, "", http.StatusOK)
	readerHTML = request(http.MethodGet, readerPath, readerToken, "", http.StatusOK).Body.String()
	if !strings.Contains(readerHTML, "重命名.pdf") || strings.Contains(readerHTML, downloadPath) || !strings.Contains(readerHTML, alternativePath) {
		t.Fatal("export denial should hide only the selected file's download link, retaining its directory entry")
	}
	request(http.MethodPut, permissionPath, ownerToken, `{"access":"denied","exportDenied":false}`, http.StatusOK)
	request(http.MethodGet, downloadPath, readerToken, "", http.StatusForbidden)
	readerHTML = request(http.MethodGet, readerPath, readerToken, "", http.StatusOK).Body.String()
	if strings.Contains(readerHTML, "重命名.pdf") {
		t.Fatal("access denial should hide the file from the reader")
	}
	var attachment models.DocumentAttachment
	if err := database.ORM.Where(&models.DocumentAttachment{AttachmentID: fileID}).Take(&attachment).Error; err != nil {
		t.Fatal(err)
	}
	for _, alternativeURL := range []string{
		filePath + "/attachments/" + fileID + "/download",
		"/uploads/" + attachment.ObjectKey,
		"/api/uploads/local/" + attachment.ObjectKey,
	} {
		result := request(http.MethodGet, alternativeURL, ownerToken, "", http.StatusOK)
		if code := decodeJSONResultCode(t, result.Body.Bytes()); code == 0 {
			t.Fatalf("private source unexpectedly available from %s", alternativeURL)
		}
	}
	var downloads int64
	if err := database.ORM.Model(&models.AuditLog{}).
		Where(&models.AuditLog{Module: "document_activity", Action: "download", TargetID: fileID, ActorUserID: &readerID}).
		Count(&downloads).Error; err != nil || downloads != 1 {
		t.Fatalf("expected one successful download audit entry, got %d: %v", downloads, err)
	}
}
