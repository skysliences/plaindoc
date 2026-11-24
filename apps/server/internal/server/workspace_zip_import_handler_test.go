package server

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
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
