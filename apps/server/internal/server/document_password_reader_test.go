package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
)

func TestRouter_DocumentPasswordGatesReaderAndExistingShare(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, ownerToken := registerAccessUser(t, serve, "document-password-reader@example.com")
	const (
		spaceID         = "01h1readerpasswordspace0001"
		nodeID          = "01h1readerpasswordnode00001"
		docID           = "01h1readerpassworddoc000001"
		routeKey        = "private-notes"
		documentContent = "reader-content-after-unlock"
		documentSecret  = "document-password-test"
		shareSecret     = "share-password-test"
	)
	seedSpaceAndDocumentForAccess(t, database, ownerID, spaceID, nodeID, docID, "public", "public")
	if err := database.ORM.Model(&models.Node{}).Where(models.NodeColumns.NodeID+" = ?", nodeID).
		Update(models.NodeColumns.ReaderSlug, routeKey).Error; err != nil {
		t.Fatal(err)
	}
	if err := database.ORM.Model(&models.Document{}).Where(models.DocumentColumns.DocumentID+" = ?", docID).
		Update(models.DocumentColumns.ContentMD, documentContent).Error; err != nil {
		t.Fatal(err)
	}
	readerPath := "/r/" + spaceID + "/" + routeKey
	sharePath := "/s/" + spaceID + "/" + routeKey
	readerExportPath := "/api/reader/spaces/" + spaceID + "/docs/" + routeKey + "/export-access"
	shareExportPath := "/api/shares/" + spaceID + "/" + routeKey + "/export-access"

	makeRequest := func(method, path, body, contentType, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		return serve(req)
	}
	get := func(path string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		return makeRequest(http.MethodGet, path, "", "", "", cookies...)
	}
	unlock := func(path, password string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
		body := url.Values{"password": {password}}.Encode()
		return makeRequest(http.MethodPost, path+"/unlock", body, "application/x-www-form-urlencoded", "", cookies...)
	}

	publishShare := makeRequest(http.MethodPut, "/api/docs/"+docID+"/share",
		`{"enabled":true,"mode":"password","password":"`+shareSecret+`"}`, "application/json", ownerToken)
	if code := decodeJSONResultCode(t, publishShare.Body.Bytes()); code != 0 {
		t.Fatalf("share setup failed with code %d: %s", code, publishShare.Body.String())
	}
	unlockedPage := get(readerPath)
	if unlockedPage.Code != http.StatusOK || !strings.Contains(unlockedPage.Body.String(), documentContent) ||
		!strings.Contains(unlockedPage.Header().Get("Cache-Control"), "no-store") {
		t.Fatalf("expected readable uncached public page before locking: %d %s", unlockedPage.Code, unlockedPage.Body.String())
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(documentSecret), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	passwords := repository.NewGormDocumentPasswordRepository(database.ORM)
	setGeneration := func() {
		t.Helper()
		err := passwords.Set(context.Background(), &models.DocumentPassword{
			DocumentID: docID, PasswordHash: string(hash), Generation: ulid.Make().String(),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	setGeneration()

	expectGate := func(page *httptest.ResponseRecorder, action string) {
		t.Helper()
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "文档查看密码") ||
			!strings.Contains(page.Body.String(), `action="`+action+`"`) ||
			strings.Contains(page.Body.String(), documentContent) ||
			!strings.Contains(page.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("expected password gate without document content: %d %s", page.Code, page.Body.String())
		}
	}
	publicGate := get(readerPath)
	expectGate(publicGate, readerPath+"/unlock")
	if strings.Contains(publicGate.Body.String(), "管理文档密码") {
		t.Fatal("guest unlock gate exposed the password management entry")
	}
	ownerGate := makeRequest(http.MethodGet, readerPath, "", "", ownerToken)
	expectGate(ownerGate, readerPath+"/unlock")
	if !strings.Contains(ownerGate.Body.String(), "管理文档密码") {
		t.Fatal("document owner lost access to password management")
	}
	if blocked := get(readerExportPath); blocked.Code != http.StatusForbidden ||
		decodeJSONResultCode(t, blocked.Body.Bytes()) != 4033 {
		t.Fatalf("expected lock to protect reader export: %d %s", blocked.Code, blocked.Body.String())
	}
	if invalid := unlock(readerPath, "incorrect-password"); invalid.Code != http.StatusForbidden ||
		!strings.Contains(invalid.Body.String(), "密码不正确") || len(invalid.Result().Cookies()) != 0 {
		t.Fatalf("expected invalid password to be rejected: %d %s", invalid.Code, invalid.Body.String())
	}
	if beforeShareProof := unlock(sharePath, documentSecret); beforeShareProof.Code != http.StatusForbidden ||
		len(beforeShareProof.Result().Cookies()) != 0 {
		t.Fatalf("expected share verification before document unlock: %d %s", beforeShareProof.Code, beforeShareProof.Body.String())
	}

	verifiedShare := makeRequest(http.MethodPost, sharePath+"/verify", `{"password":"`+shareSecret+`"}`, "application/json", "")
	if code := decodeJSONResultCode(t, verifiedShare.Body.Bytes()); code != 0 {
		t.Fatalf("expected share password acceptance: %d %s", code, verifiedShare.Body.String())
	}
	shareCookies := verifiedShare.Result().Cookies()
	expectGate(get(sharePath, shareCookies...), sharePath+"/unlock")
	if blocked := get(shareExportPath, shareCookies...); blocked.Code != http.StatusForbidden ||
		decodeJSONResultCode(t, blocked.Body.Bytes()) != 4033 {
		t.Fatalf("expected lock to protect share export: %d %s", blocked.Code, blocked.Body.String())
	}

	unlocked := unlock(readerPath, documentSecret)
	grant := unlocked.Result().Cookies()
	if unlocked.Code != http.StatusSeeOther || unlocked.Header().Get("Location") != readerPath ||
		len(grant) != 1 || grant[0].Name != service.DocumentUnlockCookieName(docID) || !grant[0].HttpOnly ||
		grant[0].Path != "/" {
		t.Fatalf("expected an HTTP-only document unlock grant and redirect: %d", unlocked.Code)
	}
	for _, path := range []string{readerPath, sharePath} {
		page := get(path, append(append([]*http.Cookie{}, grant...), shareCookies...)...)
		if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), documentContent) ||
			!strings.Contains(page.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("expected password-verified document page: %s %d %s", path, page.Code, page.Body.String())
		}
	}

	setGeneration()
	expectGate(get(readerPath, grant...), readerPath+"/unlock")
	expectGate(get(sharePath, append(grant, shareCookies...)...), sharePath+"/unlock")

	if err := database.ORM.Model(&models.Document{}).Where(models.DocumentColumns.DocumentID+" = ?", docID).
		Update(models.DocumentColumns.Visibility, models.VisibilityMember).Error; err != nil {
		t.Fatal(err)
	}
	if denied := unlock(readerPath, documentSecret); denied.Code != http.StatusForbidden ||
		len(denied.Result().Cookies()) != 0 {
		t.Fatalf("expected password to preserve document visibility: %d %s", denied.Code, denied.Body.String())
	}

	if err := database.ORM.Model(&models.Node{}).Where(models.NodeColumns.NodeID+" = ?", nodeID).
		Update(models.NodeColumns.ImportedFileKind, models.ImportedFileKindUnsupported).Error; err != nil {
		t.Fatal(err)
	}
	originalUnlockPath := "/r/" + spaceID + "/" + docID + "/unlock"
	directory := makeRequest(http.MethodGet, "/r/"+spaceID, "", "", ownerToken)
	if !strings.Contains(directory.Body.String(), originalUnlockPath) {
		t.Fatalf("locked file directory link does not lead to password validation: %s", directory.Body.String())
	}
	expectGate(makeRequest(http.MethodGet, originalUnlockPath, "", "", ownerToken), originalUnlockPath)
	originalUnlock := makeRequest(http.MethodPost, originalUnlockPath, url.Values{"password": {documentSecret}}.Encode(),
		"application/x-www-form-urlencoded", ownerToken)
	if originalUnlock.Code != http.StatusSeeOther || originalUnlock.Header().Get("Location") != originalUnlockPath ||
		len(originalUnlock.Result().Cookies()) != 1 {
		t.Fatalf("expected password submission to return to the original file unlock path: %d %s", originalUnlock.Code, originalUnlock.Body.String())
	}
	original := makeRequest(http.MethodGet, originalUnlockPath, "", "", ownerToken, originalUnlock.Result().Cookies()...)
	if original.Code != http.StatusSeeOther || original.Header().Get("Location") != "/api/docs/"+docID+"/imported-original" {
		t.Fatalf("expected redirect to the guarded original download: %d %s", original.Code, original.Body.String())
	}
}
