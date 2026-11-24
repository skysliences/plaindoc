package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_WorkspaceHiddenNodesAreOmittedFromDirectoryAndLanding(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "hidden-directory@example.com")
	const spaceID = "01h1hiddendirectory00000001"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "public")

	send := func(method, path, body string) *httptest.ResponseRecorder {
		request := httptest.NewRequest(method, path, strings.NewReader(body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		return serve(request)
	}
	type createdNode struct {
		NodeID string `json:"nodeId"`
		DocID  string `json:"docId"`
	}
	create := func(nodeType, title string, parentID *string) createdNode {
		t.Helper()
		payload, err := json.Marshal(map[string]any{"type": nodeType, "title": title, "parentId": parentID})
		if err != nil {
			t.Fatal(err)
		}
		rec := send(http.MethodPost, "/api/spaces/"+spaceID+"/nodes", string(payload))
		if rec.Code != http.StatusOK || decodeJSONResultCode(t, rec.Body.Bytes()) != response.SuccessCode {
			t.Fatalf("create test node failed: %d %s", rec.Code, rec.Body.String())
		}
		return decodeJSONResultData[createdNode](t, rec.Body.Bytes())
	}

	legacyDotFile := create("doc", "legacy dot file", nil)
	legacyDotDirectory := create("folder", "legacy dot directory", nil)
	nestedDirectory := create("folder", "nested-hidden-branch", &legacyDotDirectory.NodeID)
	nestedDocument := create("doc", "nested-hidden-document", &nestedDirectory.NodeID)
	visibleDocument := create("doc", "release.1.md", nil)

	// Seed names written before the create/update restrictions were in place.
	for _, fixture := range []struct{ nodeID, title string }{
		{legacyDotFile.NodeID, " ._README.md"},
		{legacyDotDirectory.NodeID, ".git"},
	} {
		if err := database.ORM.Model(&models.Node{}).Where(models.NodeColumns.NodeID+" = ?", fixture.nodeID).
			Update(models.NodeColumns.Title, fixture.title).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := database.ORM.Model(&models.Document{}).Where(models.DocumentColumns.DocumentID+" = ?", legacyDotFile.DocID).
		Update(models.DocumentColumns.Title, "._README.md").Error; err != nil {
		t.Fatal(err)
	}

	treePath := "/api/spaces/" + spaceID + "/tree"
	tree := send(http.MethodGet, treePath, "")
	if tree.Code != http.StatusOK || decodeJSONResultCode(t, tree.Body.Bytes()) != response.SuccessCode {
		t.Fatalf("workspace tree failed: %d %s", tree.Code, tree.Body.String())
	}
	nodes := decodeJSONResultData[[]struct{ ID string }](t, tree.Body.Bytes())
	if len(nodes) != 1 || nodes[0].ID != visibleDocument.NodeID {
		t.Fatalf("directory included a hidden node or its descendants: %s", tree.Body.String())
	}

	readerPath := "/r/" + spaceID
	landing := send(http.MethodGet, readerPath, "")
	if landing.Code != http.StatusSeeOther || landing.Header().Get("Location") != readerPath+"/"+visibleDocument.DocID {
		t.Fatalf("reader landing did not select the first visible document: %d %q", landing.Code, landing.Header().Get("Location"))
	}
	reader := send(http.MethodGet, readerPath+"/"+visibleDocument.DocID, "")
	if reader.Code != http.StatusOK || !strings.Contains(reader.Body.String(), "release.1.md") {
		t.Fatalf("visible reader failed: %d %s", reader.Code, reader.Body.String())
	}
	for _, title := range []string{"._README.md", ".git", "nested-hidden-branch", "nested-hidden-document"} {
		if strings.Contains(reader.Body.String(), title) {
			t.Errorf("reader directory unexpectedly includes %s", title)
		}
	}

	// Existing document links still use the ordinary read authorization.
	legacy := send(http.MethodGet, readerPath+"/"+nestedDocument.DocID, "")
	if legacy.Code != http.StatusOK || !strings.Contains(legacy.Body.String(), "nested-hidden-document") {
		t.Fatalf("existing reader link failed: %d %s", legacy.Code, legacy.Body.String())
	}

	rename := send(http.MethodPatch, "/api/nodes/"+legacyDotDirectory.NodeID, `{"title":"restored-folder"}`)
	if rename.Code != http.StatusOK || decodeJSONResultCode(t, rename.Body.Bytes()) != response.SuccessCode {
		t.Fatalf("rename hidden folder to a visible name failed: %d %s", rename.Code, rename.Body.String())
	}
	tree = send(http.MethodGet, treePath, "")
	if !strings.Contains(tree.Body.String(), "restored-folder") || !strings.Contains(tree.Body.String(), nestedDocument.NodeID) {
		t.Fatalf("renamed subtree did not reappear: %s", tree.Body.String())
	}
}

func TestRouter_WorkspaceRejectsDotPrefixedNodeNames(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "hidden-names@example.com")
	const spaceID = "01h1hiddendirectory00000002"
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")
	seedWorkspaceMoveNode(t, database, "folder-to-rename", spaceID, nil, "folder", "work", 1)

	for _, item := range []struct{ method, path, body string }{
		{http.MethodPost, "/api/spaces/" + spaceID + "/nodes", `{"type":"folder","title":" .git "}`},
		{http.MethodPost, "/api/spaces/" + spaceID + "/nodes", `{"type":"doc","title":"._README.md"}`},
		{http.MethodPatch, "/api/nodes/folder-to-rename", `{"title":".private"}`},
	} {
		request := httptest.NewRequest(item.method, item.path, strings.NewReader(item.body))
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Content-Type", "application/json")
		rec := serve(request)
		if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != response.ResolveErrorCode(response.CodeInvalidRequest) ||
			!strings.Contains(rec.Body.String(), response.WorkspaceErrHiddenNodeName.Message) {
			t.Errorf("expected hidden name validation for %s: code %d %s", item.body, code, rec.Body.String())
		}
	}
	var nodes []models.Node
	if err := database.ORM.Where(models.NodeColumns.SpaceID+" = ?", spaceID).Find(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 || nodes[0].Title != "work" {
		t.Fatalf("invalid request changed stored nodes: %+v", nodes)
	}
}
