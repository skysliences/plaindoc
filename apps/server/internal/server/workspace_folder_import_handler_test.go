package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_ImportDocuments_FolderImagePreviewChecksDocumentAndShareAccess(t *testing.T) {
	t.Chdir(t.TempDir())
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "folder-image-owner@example.com")
	spaceID := "folder-images-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatal(err)
	}
	body, contentType := buildWorkspaceImportMultipartBodyWithOptions(t, "", map[string][]byte{
		"image.png": pngBytes.Bytes(),
	}, map[string]string{"folderManifest": `{"paths":["素材/子文件夹/image.png"]}`})
	req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", contentType)
	rec := serve(req)
	payload := decodeJSONResultData[struct {
		CreatedNodes []struct {
			DocumentID       string `json:"documentId"`
			ImportedFileKind string `json:"importedFileKind"`
		} `json:"createdNodes"`
		FailedCount int `json:"failedCount"`
	}](t, rec.Body.Bytes())
	if len(payload.CreatedNodes) != 3 || payload.FailedCount != 0 || payload.CreatedNodes[2].ImportedFileKind != "image" {
		t.Fatalf("image did not import as an openable tree entry: %s", rec.Body.String())
	}
	documentID := payload.CreatedNodes[2].DocumentID
	documentURL := "/api/docs/" + documentID
	previewURL := documentURL + "/imported-image"
	ownerRequest := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
		req.Header.Set("Content-Type", "application/json")
		return serve(req)
	}
	imageDocument := decodeJSONResultData[map[string]any](t, ownerRequest(http.MethodGet, documentURL, "").Body.Bytes())
	if imageDocument["importedImageUrl"] != previewURL || imageDocument["importedFileKind"] != "image" {
		t.Fatalf("missing preview metadata: %+v", imageDocument)
	}
	checkImage := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" ||
			!bytes.Equal(rec.Body.Bytes(), pngBytes.Bytes()) ||
			!strings.Contains(rec.Header().Get("Cache-Control"), "no-store") {
			t.Fatalf("expected permission-scoped PNG bytes, got %d %s with %d bytes", rec.Code, rec.Header().Get("Content-Type"), rec.Body.Len())
		}
	}
	checkBlocked := func(rec *httptest.ResponseRecorder) {
		t.Helper()
		if bytes.Equal(rec.Body.Bytes(), pngBytes.Bytes()) || strings.HasPrefix(rec.Header().Get("Content-Type"), "image/") {
			t.Fatalf("protected preview was exposed: %d %s", rec.Code, rec.Header().Get("Content-Type"))
		}
	}
	checkImage(ownerRequest(http.MethodGet, previewURL, ""))
	checkBlocked(serve(httptest.NewRequest(http.MethodGet, previewURL, nil)))

	var attachment models.DocumentAttachment
	if err := database.ORM.Where("attachment_id = ?", documentID).First(&attachment).Error; err != nil {
		t.Fatal(err)
	}
	for _, prefix := range []string{"/uploads/", "/uploads/local/", "/api/uploads/"} {
		checkBlocked(serve(httptest.NewRequest(http.MethodGet, prefix+attachment.ObjectKey, nil)))
	}
	list := decodeJSONResultData[struct{ Items []any }](t, ownerRequest(http.MethodGet, documentURL+"/attachments", "").Body.Bytes())
	if len(list.Items) != 0 {
		t.Fatal("private preview source appeared in ordinary attachment list")
	}
	if rec := ownerRequest(http.MethodPut, documentURL, `{"contentMd":"text","baseVersion":1}`); decodeJSONResultCode(t, rec.Body.Bytes()) == 0 {
		t.Fatalf("accepted Markdown body for an image: %s", rec.Body.String())
	}

	shareResult := ownerRequest(http.MethodPut, documentURL+"/share", `{"enabled":true,"mode":"password","password":"folder-preview-password"}`)
	if code := decodeJSONResultCode(t, shareResult.Body.Bytes()); code != 0 {
		t.Fatalf("create image share code = %d: %s", code, shareResult.Body.String())
	}
	sharePath := spaceID + "/" + documentID
	shareImageURL := "/api/shares/" + sharePath + "/imported-image"
	checkBlocked(serve(httptest.NewRequest(http.MethodGet, shareImageURL, nil)))
	verify := httptest.NewRequest(http.MethodPost, "/s/"+sharePath+"/verify", strings.NewReader(`{"password":"folder-preview-password"}`))
	verify.Header.Set("Content-Type", "application/json")
	verified := serve(verify)
	if code := decodeJSONResultCode(t, verified.Body.Bytes()); code != 0 {
		t.Fatalf("verify image share code = %d: %s", code, verified.Body.String())
	}
	shareRequest := httptest.NewRequest(http.MethodGet, shareImageURL, nil)
	for _, cookie := range verified.Result().Cookies() {
		shareRequest.AddCookie(cookie)
	}
	checkImage(serve(shareRequest))
}

func TestRouter_ImportDocuments_FolderPreservesTreeAndEditableText(t *testing.T) {
	t.Chdir(t.TempDir())
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })

	userID, _, token := registerAccessUser(t, serve, "folder-import-owner@example.com")
	spaceID := "folder-import-space"
	seedSpaceForWorkspaceCreateNode(t, database, userID, spaceID, "member")
	spaceURL := "/api/spaces/" + spaceID
	apiRequest := func(method string, target string, content string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, target, strings.NewReader(content))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		return serve(req)
	}

	rawText := "  # 保留首尾空白\n<b>文本</b>\n\n"
	const invalidPNG = "\x89PNG\x00\xff"
	sourcePaths := []string{"工作资料/README.md", "工作资料/子目录/记录.txt", "工作资料/子目录/图像.png"}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("folderManifest", `{"paths":["工作资料/README.md","工作资料/子目录/记录.txt","工作资料/子目录/图像.png"],"directories":["工作资料/空目录"]}`); err != nil {
		t.Fatal(err)
	}
	for index, content := range []string{"# 欢迎", rawText, invalidPNG} {
		part, err := form.CreateFormFile("files", path.Base(sourcePaths[index]))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, spaceURL+"/imports", &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	imported := serve(req)
	if code := decodeJSONResultCode(t, imported.Body.Bytes()); code != 0 {
		t.Fatalf("import folder code = %d, body=%s", code, imported.Body.String())
	}
	receipt := decodeJSONResultData[struct {
		FailedCount int `json:"failedCount"`
	}](t, imported.Body.Bytes())
	if receipt.FailedCount != 0 {
		t.Fatalf("folder upload returned failed items: %s", imported.Body.String())
	}

	type treeNode struct {
		ID               string     `json:"id"`
		DocumentID       string     `json:"documentId"`
		DocumentRouteKey string     `json:"documentRouteKey"`
		Type             string     `json:"type"`
		Title            string     `json:"title"`
		ImportedFileKind string     `json:"importedFileKind"`
		Children         []treeNode `json:"children"`
	}
	tree := decodeJSONResultData[[]treeNode](t, apiRequest(http.MethodGet, spaceURL+"/tree", "").Body.Bytes())
	findNode := func(nodes []treeNode, title string) treeNode {
		t.Helper()
		for _, node := range nodes {
			if node.Title == title {
				return node
			}
		}
		t.Fatalf("missing child %q in %+v", title, nodes)
		return treeNode{}
	}
	root := findNode(tree, "工作资料")
	empty := findNode(root.Children, "空目录")
	child := findNode(root.Children, "子目录")
	markdown := findNode(root.Children, "README.md")
	plain := findNode(child.Children, "记录.txt")
	unsupported := findNode(child.Children, "图像.png")
	if root.Type != "folder" || empty.Type != "folder" || child.Type != "folder" || len(empty.Children) != 0 ||
		markdown.Type != "doc" || len(markdown.Children) != 0 {
		t.Fatalf("unexpected tree shape: %+v", root)
	}
	if plain.ImportedFileKind != "text" || plain.DocumentID == "" || plain.DocumentRouteKey == "" {
		t.Fatalf("plain text is not openable: %+v", plain)
	}
	if unsupported.ImportedFileKind != "unsupported" || unsupported.Type != "doc" || unsupported.DocumentID != unsupported.ID || unsupported.DocumentRouteKey != "" {
		t.Fatalf("unexpected unsupported node: %+v", unsupported)
	}
	assertWorkspaceImportedOriginal(t, database, unsupported.DocumentID, unsupported.Title, []byte(invalidPNG))
	if rec := apiRequest(http.MethodGet, "/api/docs/"+unsupported.ID, ""); decodeJSONResultCode(t, rec.Body.Bytes()) == 0 {
		t.Fatalf("unsupported file resolved as a document: %s", rec.Body.String())
	}
	createChild := `{"type":"folder","title":"错误目标","parentId":"` + unsupported.ID + `"}`
	if rec := apiRequest(http.MethodPost, spaceURL+"/nodes", createChild); decodeJSONResultCode(t, rec.Body.Bytes()) == 0 {
		t.Fatalf("unsupported file accepted as parent: %s", rec.Body.String())
	}

	type textDocument struct {
		ImportedFileKind string `json:"importedFileKind"`
		ContentMD        string `json:"contentMd"`
		Version          int    `json:"version"`
	}
	documentURL := "/api/docs/" + plain.DocumentID
	got := decodeJSONResultData[textDocument](t, apiRequest(http.MethodGet, documentURL, "").Body.Bytes())
	if got.ImportedFileKind != "text" || got.ContentMD != rawText || got.Version != 1 {
		t.Fatalf("original text was altered: %+v", got)
	}

	reader := apiRequest(http.MethodGet, "/r/"+spaceID+"/"+plain.DocumentRouteKey, "")
	if reader.Code != http.StatusOK {
		t.Fatalf("reader failed with status %d: %s", reader.Code, reader.Body.String())
	}
	state := decodeReaderInitialState(t, reader.Body.String())
	readerDocument, ok := state["document"].(map[string]any)
	if !ok || readerDocument["importedFileKind"] != "text" || readerDocument["contentMd"] != rawText {
		t.Fatalf("reader metadata is incorrect: %+v", readerDocument)
	}
	if !strings.Contains(reader.Body.String(), `"importedFileKind":"unsupported"`) {
		t.Fatalf("reader tree did not include unsupported file metadata: %s", reader.Body.String())
	}

	newText := "新内容\n"
	saveBody, err := json.Marshal(map[string]any{"contentMd": newText, "baseVersion": 1})
	if err != nil {
		t.Fatal(err)
	}
	type documentResponse struct {
		Document textDocument `json:"document"`
	}
	saved := decodeJSONResultData[documentResponse](t, apiRequest(http.MethodPut, documentURL, string(saveBody)).Body.Bytes())
	if saved.Document.ImportedFileKind != "text" || saved.Document.ContentMD != newText || saved.Document.Version != 2 {
		t.Fatalf("unexpected save response: %+v", saved)
	}
	var firstRevisionID string
	if err := database.ORM.Table("document_revisions").
		Select("document_revision_id").
		Where("document_id = ? AND version = ?", plain.DocumentID, 1).
		Scan(&firstRevisionID).Error; err != nil || firstRevisionID == "" {
		t.Fatalf("cannot find text revision: %v", err)
	}
	restored := decodeJSONResultData[documentResponse](t, apiRequest(http.MethodPost, documentURL+"/revisions/"+firstRevisionID+"/restore", `{"baseVersion":2}`).Body.Bytes())
	if restored.Document.ImportedFileKind != "text" || restored.Document.ContentMD != rawText || restored.Document.Version != 3 {
		t.Fatalf("restore did not preserve the original text: %+v", restored)
	}
}

func TestRouter_ImportDocuments_FolderRejectsPathsOutsideSelectedRoot(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "invalid-folder-import-owner@example.com")
	spaceID := "invalid-folder-import-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	for _, manifest := range []string{
		`{"paths":[],"directories":["资料","资料/../相邻目录"]}`,
		`{"paths":[],"directories":["资料","相邻目录/空"]}`,
	} {
		body, contentType := buildWorkspaceImportMultipartBodyWithOptions(t, "", nil, map[string]string{"folderManifest": manifest})
		req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", contentType)
		rec := serve(req)
		if decodeJSONResultCode(t, rec.Body.Bytes()) == 0 {
			t.Fatalf("accepted invalid folder manifest: %s", manifest)
		}
	}
	var nodeCount int64
	if err := database.ORM.Table("nodes").Where("space_id = ?", spaceID).Count(&nodeCount).Error; err != nil {
		t.Fatal(err)
	}
	if nodeCount != 0 {
		t.Fatalf("invalid import created %d nodes", nodeCount)
	}
}
