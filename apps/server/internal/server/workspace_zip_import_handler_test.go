package server

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_ImportDocuments_ExtractsZIPsIntoNamedDirectories(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "zip-directory-import@example.com")
	spaceID := "standalone-zip-directory-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	inner := buildWorkspaceImportZIP(t, map[string][]byte{"chapter.md": []byte("# Nested document")})
	outer := buildWorkspaceImportZIP(t, map[string][]byte{
		"intro.md":             []byte("# Outer document"),
		"attachments/part.ZIP": inner,
		"empty/":               nil,
	})
	body, contentType := buildWorkspaceImportMultipartBody(t, "", map[string][]byte{
		"lecture.v2.ZIP": outer,
		"blank.zip":      buildWorkspaceImportZIP(t, nil),
		"sibling.md":     []byte("# Normal file"),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", contentType)
	rec := serve(request)
	if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != 0 {
		t.Fatalf("import ZIP files returned code %d: %s", code, rec.Body.String())
	}
	result := decodeJSONResultData[struct {
		FailedCount int `json:"failedCount"`
	}](t, rec.Body.Bytes())
	if result.FailedCount != 0 {
		t.Fatalf("import returned failed items: %s", rec.Body.String())
	}

	archiveID := findWorkspaceImportedZIPFolder(t, database, spaceID, "lecture.v2", "")
	findWorkspaceImportedZIPFolder(t, database, spaceID, "blank", "")
	findWorkspaceImportedZIPFolder(t, database, spaceID, "empty", archiveID)
	attachmentsID := findWorkspaceImportedZIPFolder(t, database, spaceID, "attachments", archiveID)
	partID := findWorkspaceImportedZIPFolder(t, database, spaceID, "part", attachmentsID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "intro", models.NodeTypeDoc, archiveID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "chapter", models.NodeTypeDoc, partID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "sibling", models.NodeTypeDoc, "")
	assertWorkspaceImportHasNoZIPNodes(t, database, spaceID)
}

func TestRouter_ImportDocuments_PopulatesZIPsContainingOnlyOtherFileTypes(t *testing.T) {
	t.Chdir(t.TempDir())
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "zip-non-document-files@example.com")
	spaceID := "zip-non-document-files-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	const tsvContent = "name\tquantity\nnotebook\t2\n"
	const jsonContent = "{\"enabled\":true}\n"
	const goContent = "package example\n"
	const pdfContent = "%PDF-1.4\n"
	importStandaloneZIPFixture(t, serve, spaceID, token, "project.zip", map[string][]byte{
		"reports/data.tsv":      []byte(tsvContent),
		"reports/options.json":  []byte(jsonContent),
		"src/main.go":           []byte(goContent),
		"images/diagram.png":    buildWorkspaceImportedZIPPNG(t),
		"reference/summary.pdf": []byte(pdfContent),
	})

	archiveID := findWorkspaceImportedZIPFolder(t, database, spaceID, "project", "")
	reportsID := findWorkspaceImportedZIPFolder(t, database, spaceID, "reports", archiveID)
	sourceID := findWorkspaceImportedZIPFolder(t, database, spaceID, "src", archiveID)
	imagesID := findWorkspaceImportedZIPFolder(t, database, spaceID, "images", archiveID)
	referenceID := findWorkspaceImportedZIPFolder(t, database, spaceID, "reference", archiveID)

	for _, item := range []struct {
		name, parentID, content string
	}{
		{"data.tsv", reportsID, tsvContent},
		{"options.json", reportsID, jsonContent},
		{"main.go", sourceID, goContent},
	} {
		node := findWorkspaceZIPImportTreeNode(t, database, spaceID, item.name, models.NodeTypeDoc, item.parentID)
		if node.ImportedFileKind != models.ImportedFileKindText {
			t.Fatalf("expected %s to be readable text, got %q", item.name, node.ImportedFileKind)
		}
		if content := readWorkspaceImportedZIPDocument(t, database, node.NodeID); content != item.content {
			t.Fatalf("expected exact %s contents, got %q", item.name, content)
		}
	}
	imageNode := findWorkspaceZIPImportTreeNode(t, database, spaceID, "diagram.png", models.NodeTypeDoc, imagesID)
	if imageNode.ImportedFileKind != models.ImportedFileKindImage {
		t.Fatalf("expected an image node, got %q", imageNode.ImportedFileKind)
	}
	binaryNode := findWorkspaceZIPImportTreeNode(t, database, spaceID, "summary.pdf", models.NodeTypeDoc, referenceID)
	if binaryNode.ImportedFileKind != models.ImportedFileKindUnsupported {
		t.Fatalf("expected a visible unsupported-file node, got %q", binaryNode.ImportedFileKind)
	}
	assertWorkspaceImportedOriginal(t, database, binaryNode.NodeID, "summary.pdf", []byte(pdfContent))
	assertWorkspaceImportHasNoZIPNodes(t, database, spaceID)
}

func assertWorkspaceImportedOriginal(t *testing.T, database *storage.Database, documentID, fileName string, want []byte) {
	t.Helper()
	var attachment models.DocumentAttachment
	if err := database.ORM.Where("attachment_id = ?", documentID).First(&attachment).Error; err != nil {
		t.Fatalf("find stored original for %q: %v", fileName, err)
	}
	if attachment.FileName != fileName || attachment.DocumentID != documentID || attachment.PreviewKind != "none" ||
		attachment.MimeType != "application/octet-stream" || !models.IsImportedOriginalObjectKey(attachment.ObjectKey) {
		t.Fatalf("incorrect original metadata for %q: %+v", fileName, attachment)
	}
	content, err := os.ReadFile(filepath.Join("uploads", filepath.FromSlash(attachment.ObjectKey)))
	if err != nil || !bytes.Equal(content, want) {
		t.Fatalf("stored original %q differs from upload: bytes=%q error=%v", fileName, content, err)
	}
}

func TestRouter_ImportDocuments_KeepsMixedZIPFilesBesideMarkdown(t *testing.T) {
	t.Chdir(t.TempDir())
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "zip-mixed-files@example.com")
	spaceID := "zip-mixed-files-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	image := buildWorkspaceImportedZIPPNG(t)
	importStandaloneZIPFixture(t, serve, spaceID, token, "mixed.zip", map[string][]byte{
		"overview.md":          []byte("# Project\n\n![Cover](assets/linked.png)\n"),
		"assets/linked.png":    image,
		"assets/unlinked.png":  image,
		"unreferenced.png":     image,
		"configs/options.json": []byte("{\"enabled\":true}\n"),
		"album.zip":            buildWorkspaceImportZIP(t, map[string][]byte{"assets/photo.png": image}),
	})

	archiveID := findWorkspaceImportedZIPFolder(t, database, spaceID, "mixed", "")
	markdown := findWorkspaceZIPImportTreeNode(t, database, spaceID, "overview", models.NodeTypeDoc, archiveID)
	if content := readWorkspaceImportedZIPDocument(t, database, markdown.NodeID); !strings.Contains(content, "![Cover](/uploads/") {
		t.Fatalf("expected a localized Markdown image, got %q", content)
	}
	imageNode := findWorkspaceZIPImportTreeNode(t, database, spaceID, "unreferenced.png", models.NodeTypeDoc, archiveID)
	if imageNode.ImportedFileKind != models.ImportedFileKindImage {
		t.Fatalf("expected a standalone image node, got %q", imageNode.ImportedFileKind)
	}
	configsID := findWorkspaceImportedZIPFolder(t, database, spaceID, "configs", archiveID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "options.json", models.NodeTypeDoc, configsID)
	albumID := findWorkspaceImportedZIPFolder(t, database, spaceID, "album", archiveID)
	assetsID := findWorkspaceImportedZIPFolder(t, database, spaceID, "assets", albumID)
	photo := findWorkspaceZIPImportTreeNode(t, database, spaceID, "photo.png", models.NodeTypeDoc, assetsID)
	if photo.ImportedFileKind != models.ImportedFileKindImage {
		t.Fatalf("expected the image from the nested archive, got %q", photo.ImportedFileKind)
	}
	archiveAssetsID := findWorkspaceImportedZIPFolder(t, database, spaceID, "assets", archiveID)
	for _, title := range []string{"linked.png", "unlinked.png"} {
		node := findWorkspaceZIPImportTreeNode(t, database, spaceID, title, models.NodeTypeDoc, archiveAssetsID)
		if node.ImportedFileKind != models.ImportedFileKindImage {
			t.Fatalf("expected previewable image %q, got %q", title, node.ImportedFileKind)
		}
	}
	assertWorkspaceImportHasNoZIPNodes(t, database, spaceID)
}

func TestRouter_ImportDocuments_ExpandsZIPsFromFolderUpload(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "folder-zip-import@example.com")
	spaceID := "folder-zip-import-space"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	inner := buildWorkspaceImportZIP(t, map[string][]byte{"nested.md": []byte("# Nested")})
	archive := buildWorkspaceImportZIP(t, map[string][]byte{
		"guide/README.md": []byte("# Guide"),
		"guide/inner.zip": inner,
		"empty/":          nil,
		".git/secret.md":  []byte("# Hidden"),
	})
	files := []struct {
		name    string
		content []byte
	}{
		{"upload/selected/report.v2.zip", archive},
		{"upload/selected/broken.zip", []byte("invalid archive")},
		{"upload/selected/summary.md", []byte("# A visible sibling")},
		{"upload/selected/manual.zip", buildWorkspaceImportZIP(t, map[string][]byte{"archived.md": []byte("# From the archive")})},
		{"upload/selected/manual/local.md", []byte("# From the directory")},
	}
	paths := make([]string, 0, len(files))
	for _, file := range files {
		paths = append(paths, file.name)
	}
	manifest, err := json.Marshal(struct {
		Paths []string `json:"paths"`
	}{Paths: paths})
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("folderManifest", string(manifest)); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		part, err := writer.CreateFormFile("files", path.Base(file.name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(file.content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", &body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	rec := serve(request)
	if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != 0 {
		t.Fatalf("folder import returned code %d: %s", code, rec.Body.String())
	}
	result := decodeJSONResultData[struct {
		FailedCount  int `json:"failedCount"`
		FailureItems []struct {
			SourcePath string `json:"sourcePath"`
		} `json:"failureItems"`
	}](t, rec.Body.Bytes())
	if result.FailedCount != 1 || len(result.FailureItems) != 1 || result.FailureItems[0].SourcePath != "upload/selected/broken.zip" {
		t.Fatalf("expected only the invalid archive to fail: %s", rec.Body.String())
	}

	uploadID := findWorkspaceImportedZIPFolder(t, database, spaceID, "upload", "")
	selectedID := findWorkspaceImportedZIPFolder(t, database, spaceID, "selected", uploadID)
	archiveID := findWorkspaceImportedZIPFolder(t, database, spaceID, "report.v2", selectedID)
	guideID := findWorkspaceImportedZIPFolder(t, database, spaceID, "guide", archiveID)
	innerID := findWorkspaceImportedZIPFolder(t, database, spaceID, "inner", guideID)
	findWorkspaceImportedZIPFolder(t, database, spaceID, "empty", archiveID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "README.md", models.NodeTypeDoc, guideID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "nested.md", models.NodeTypeDoc, innerID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "summary.md", models.NodeTypeDoc, selectedID)
	manualDirectoryID := findWorkspaceImportedZIPFolder(t, database, spaceID, "manual", selectedID)
	manualArchiveID := findWorkspaceImportedZIPFolder(t, database, spaceID, "manual (1)", selectedID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "local.md", models.NodeTypeDoc, manualDirectoryID)
	findWorkspaceZIPImportTreeNode(t, database, spaceID, "archived.md", models.NodeTypeDoc, manualArchiveID)
	assertWorkspaceImportHasNoZIPNodes(t, database, spaceID)
	var invalidNodes int64
	if err := database.ORM.Model(&models.Node{}).
		Where(models.NodeColumns.SpaceID+" = ?", spaceID).
		Where(models.NodeColumns.Title+" IN ?", []string{"broken", ".git", "secret.md"}).
		Count(&invalidNodes).Error; err != nil || invalidNodes != 0 {
		t.Fatalf("expected no invalid or hidden nodes, found %d: %v", invalidNodes, err)
	}
}

func buildWorkspaceImportedZIPPNG(t *testing.T) []byte {
	t.Helper()
	var fixture bytes.Buffer
	if err := png.Encode(&fixture, image.NewRGBA(image.Rect(0, 0, 2, 1))); err != nil {
		t.Fatalf("build PNG fixture: %v", err)
	}
	return fixture.Bytes()
}

func importStandaloneZIPFixture(
	t *testing.T,
	serve func(*http.Request) *httptest.ResponseRecorder,
	spaceID, token, name string,
	files map[string][]byte,
) {
	t.Helper()
	body, contentType := buildWorkspaceImportMultipartBody(t, "", map[string][]byte{
		name: buildWorkspaceImportZIP(t, files),
	})
	request := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", contentType)
	rec := serve(request)
	if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != 0 {
		t.Fatalf("ZIP import returned code %d: %s", code, rec.Body.String())
	}
	result := decodeJSONResultData[struct {
		FailedCount int `json:"failedCount"`
	}](t, rec.Body.Bytes())
	if result.FailedCount != 0 {
		t.Fatalf("ZIP import returned failed items: %s", rec.Body.String())
	}
}

func readWorkspaceImportedZIPDocument(t *testing.T, database *storage.Database, nodeID string) string {
	t.Helper()
	var document models.Document
	if err := database.ORM.Where(models.DocumentColumns.NodeID+" = ?", nodeID).Take(&document).Error; err != nil {
		t.Fatalf("read imported document for node %q: %v", nodeID, err)
	}
	return document.ContentMD
}

func findWorkspaceImportedZIPFolder(t *testing.T, database *storage.Database, spaceID, title, parentID string) string {
	t.Helper()
	return findWorkspaceZIPImportTreeNode(t, database, spaceID, title, models.NodeTypeFolder, parentID).NodeID
}

func findWorkspaceZIPImportTreeNode(t *testing.T, database *storage.Database, spaceID, title string, kind models.NodeType, parentID string) models.Node {
	t.Helper()
	query := database.ORM.Model(&models.Node{}).
		Where(models.NodeColumns.SpaceID+" = ?", spaceID).
		Where(models.NodeColumns.Title+" = ?", title).
		Where(models.NodeColumns.Type+" = ?", kind)
	if parentID == "" {
		query = query.Where(models.NodeColumns.ParentNodeID + " IS NULL")
	} else {
		query = query.Where(models.NodeColumns.ParentNodeID+" = ?", parentID)
	}
	var node models.Node
	if err := query.Take(&node).Error; err != nil {
		t.Fatalf("missing %s %q under %q: %v", kind, title, parentID, err)
	}
	return node
}

func assertWorkspaceImportHasNoZIPNodes(t *testing.T, database *storage.Database, spaceID string) {
	t.Helper()
	var archiveCount int64
	if err := database.ORM.Model(&models.Node{}).
		Where(models.NodeColumns.SpaceID+" = ?", spaceID).
		Where("LOWER("+models.NodeColumns.Title+") LIKE ?", "%.zip").
		Count(&archiveCount).Error; err != nil || archiveCount != 0 {
		t.Fatalf("expected no ZIP entries in imported tree, found %d: %v", archiveCount, err)
	}
}
