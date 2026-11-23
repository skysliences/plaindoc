package server

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_WorkspaceCollaboration(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })

	seedHomepageSearchConfig(t, database)
	ownerID, _, ownerToken := registerAccessUser(t, serve, "workspace-owner@example.com")
	memberID, memberEmail, memberToken := registerAccessUser(t, serve, "workspace-member@example.com")

	request := func(method, path, token, body string, want int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		rec := serve(req)
		if rec.Code != want {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, rec.Code, want, rec.Body.String())
		}
		return rec
	}

	home := request(http.MethodGet, "/", ownerToken, "", http.StatusOK).Body.String()
	if !strings.Contains(home, "新建空间") || !strings.Contains(home, "退出登录") || strings.Contains(home, "我的空间") {
		t.Fatal("homepage must show the create and logout controls without a workspace menu shortcut")
	}
	if strings.Contains(home, "后台管理") {
		t.Fatal("ordinary user homepage exposes the admin menu")
	}

	createdSpace := request(http.MethodPost, "/api/spaces", ownerToken, `{"name":"项目空间"}`, http.StatusOK)
	space := decodeJSONResultData[struct{ ID string }](t, createdSpace.Body.Bytes())
	spacePath := "/api/spaces/" + space.ID
	request(http.MethodPatch, spacePath, ownerToken, `{"name":"团队空间"}`, http.StatusOK)

	createDocument := func(title string) string {
		t.Helper()
		rec := request(http.MethodPost, spacePath+"/nodes", ownerToken, `{"type":"doc","title":"`+title+`"}`, http.StatusOK)
		return decodeJSONResultData[struct{ DocID string }](t, rec.Body.Bytes()).DocID
	}
	protectedDoc := createDocument("权限测试文档")
	otherDoc := createDocument("其他文档")
	docPath := "/api/docs/" + protectedDoc
	memberPath := spacePath + "/members"
	permissionPath := docPath + "/permissions/" + memberID

	request(http.MethodGet, docPath, memberToken, "", http.StatusForbidden)
	request(http.MethodPut, memberPath, memberToken, `{"email":"`+memberEmail+`","role":"collaborator"}`, http.StatusForbidden)
	request(http.MethodPut, memberPath, ownerToken, `{"email":"`+memberEmail+`","role":"reader"}`, http.StatusOK)
	mySpaces := request(http.MethodGet, "/api/my-spaces", memberToken, "", http.StatusOK).Body.String()
	if !strings.Contains(mySpaces, space.ID) || !strings.Contains(mySpaces, "团队空间") {
		t.Fatalf("invited member cannot find the renamed space: %s", mySpaces)
	}
	request(http.MethodGet, docPath, memberToken, "", http.StatusOK)
	request(http.MethodPut, docPath, memberToken, `{"contentMd":"不可保存","baseVersion":1}`, http.StatusForbidden)
	request(http.MethodPut, permissionPath, memberToken, `{"access":"edit"}`, http.StatusForbidden)

	exportPath := "/api/reader/spaces/" + space.ID + "/docs/" + protectedDoc + "/export-access"
	request(http.MethodGet, exportPath, memberToken, "", http.StatusOK)
	request(http.MethodPut, permissionPath, memberToken, `{"exportDenied":true}`, http.StatusForbidden)
	request(http.MethodPut, permissionPath, ownerToken, `{"exportDenied":true}`, http.StatusOK)
	request(http.MethodGet, exportPath, memberToken, "", http.StatusForbidden)
	request(http.MethodGet, exportPath, ownerToken, "", http.StatusOK)
	request(http.MethodGet, docPath, memberToken, "", http.StatusOK)
	request(http.MethodPut, docPath, memberToken, `{"contentMd":"仍不可保存","baseVersion":1}`, http.StatusForbidden)

	request(http.MethodPut, permissionPath, ownerToken, `{"access":"read_no_copy"}`, http.StatusOK)
	for _, path := range []string{docPath, "/r/" + space.ID + "/" + protectedDoc} {
		if result := request(http.MethodGet, path, memberToken, "", http.StatusOK); !strings.Contains(result.Body.String(), `"copyDenied":true`) {
			t.Fatalf("document response must expose the copy restriction for %s", path)
		}
	}
	request(http.MethodPost, docPath+"/copy-events", memberToken, "", http.StatusForbidden)
	request(http.MethodPut, permissionPath, ownerToken, `{"access":"edit_no_copy"}`, http.StatusOK)
	if tree := request(http.MethodGet, spacePath+"/tree", memberToken, "", http.StatusOK); !strings.Contains(tree.Body.String(), protectedDoc) {
		t.Fatal("blocking copy must preserve document edit access")
	}
	request(http.MethodPost, docPath+"/copy-events", memberToken, "", http.StatusForbidden)

	request(http.MethodPut, permissionPath, ownerToken, `{"access":"edit"}`, http.StatusOK)
	request(http.MethodGet, exportPath, memberToken, "", http.StatusForbidden)
	request(http.MethodPut, permissionPath, ownerToken, `{"exportDenied":false}`, http.StatusOK)
	request(http.MethodGet, exportPath, memberToken, "", http.StatusOK)
	request(http.MethodPost, docPath+"/copy-events", memberToken, "", http.StatusOK)
	request(http.MethodPost, docPath+"/copy-events", "", "", http.StatusForbidden)
	permissionList := request(http.MethodGet, memberPath+"?documentId="+protectedDoc, ownerToken, "", http.StatusOK).Body.String()
	if !strings.Contains(permissionList, `"access":"edit"`) {
		t.Fatalf("document permission list is missing the configured access: %s", permissionList)
	}
	tree := request(http.MethodGet, spacePath+"/tree", memberToken, "", http.StatusOK).Body.String()
	if !strings.Contains(tree, protectedDoc) || strings.Contains(tree, otherDoc) {
		t.Fatalf("editor tree for a member with one editable document is incorrect: %s", tree)
	}
	request(http.MethodPut, docPath, memberToken, `{"contentMd":"已修改","baseVersion":1}`, http.StatusOK)
	request(http.MethodPut, "/api/docs/"+otherDoc, memberToken, `{"contentMd":"不可保存","baseVersion":1}`, http.StatusForbidden)
	readerPath := "/r/" + space.ID + "/" + protectedDoc
	if reader := request(http.MethodGet, readerPath, memberToken, "", http.StatusOK); strings.Contains(reader.Body.String(), "create=document") {
		t.Fatal("document edit permission alone must not allow creating space documents")
	}

	request(http.MethodPut, memberPath, ownerToken, `{"email":"`+memberEmail+`","role":"collaborator"}`, http.StatusOK)
	request(http.MethodPut, permissionPath, ownerToken, `{"access":"read"}`, http.StatusOK)
	request(http.MethodPut, docPath, memberToken, `{"contentMd":"不可保存","baseVersion":2}`, http.StatusForbidden)
	request(http.MethodGet, docPath, memberToken, "", http.StatusOK)
	readerHTML := request(http.MethodGet, readerPath, memberToken, "", http.StatusOK).Body.String()
	if strings.Contains(readerHTML, "/"+protectedDoc+"?from=reader") {
		t.Fatal("read-only documents must not receive an edit link")
	}
	if !strings.Contains(readerHTML, "create=document") {
		t.Fatal("space collaborators must be able to create documents while viewing a read-only document")
	}
	searchPath := "/search?q=" + url.QueryEscape("权限测试文档")
	search := request(http.MethodGet, searchPath, memberToken, "", http.StatusOK).Body.String()
	if !strings.Contains(search, readerPath) {
		t.Fatalf("readable document is missing from search results: %s", search)
	}
	request(http.MethodPut, permissionPath, ownerToken, `{"access":"denied"}`, http.StatusOK)
	request(http.MethodPut, docPath+"/theme", memberToken, `{"themeId":"default"}`, http.StatusForbidden)
	request(http.MethodDelete, "/api/nodes/"+protectedDoc, memberToken, "", http.StatusForbidden)
	search = request(http.MethodGet, searchPath, memberToken, "", http.StatusOK).Body.String()
	if strings.Contains(search, readerPath) {
		t.Fatalf("denied document is visible in search results: %s", search)
	}
	request(http.MethodGet, docPath, memberToken, "", http.StatusForbidden)
	request(http.MethodGet, readerPath, memberToken, "", http.StatusForbidden)
	tree = request(http.MethodGet, spacePath+"/tree", memberToken, "", http.StatusOK).Body.String()
	if strings.Contains(tree, protectedDoc) || !strings.Contains(tree, otherDoc) {
		t.Fatalf("editor tree contains a document hidden from the member: %s", tree)
	}

	request(http.MethodPut, permissionPath, ownerToken, `{"access":"inherit"}`, http.StatusOK)
	request(http.MethodPut, docPath, memberToken, `{"contentMd":"继承空间写权限","baseVersion":2}`, http.StatusOK)
	request(http.MethodDelete, memberPath+"/"+memberID, ownerToken, "", http.StatusOK)
	request(http.MethodGet, docPath, memberToken, "", http.StatusForbidden)

	if err := database.ORM.Create(&models.UserAdminRole{UserID: ownerID, Role: models.AdminRolePlatformAdmin}).Error; err != nil {
		t.Fatal(err)
	}
	auditPath := "/api/admin/audits?module=document_activity&actorUserId=" + memberID + "&targetId=" + protectedDoc
	request(http.MethodGet, auditPath, memberToken, "", http.StatusForbidden)
	for action, want := range map[string]int64{"edit": 2, "copy": 1} {
		audit := request(http.MethodGet, auditPath+"&action="+action, ownerToken, "", http.StatusOK)
		data := decodeJSONResultData[struct {
			Pagination struct{ Total int64 }
		}](t, audit.Body.Bytes())
		if data.Pagination.Total != want {
			t.Fatalf("expected %d %s events for the document and member, got %d", want, action, data.Pagination.Total)
		}
	}
	if audit := request(http.MethodGet, auditPath+"&action=read", ownerToken, "", http.StatusOK); !strings.Contains(audit.Body.String(), "打开文档") {
		t.Fatal("missing authorized document reads in the activity log")
	}

	home = request(http.MethodGet, "/", ownerToken, "", http.StatusOK).Body.String()
	if !strings.Contains(home, "后台管理") {
		t.Fatal("platform administrator homepage is missing the admin menu")
	}
}

func TestRouter_HomeCreateSpaceStartsWithExampleDocument(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	_, _, token := registerAccessUser(t, serve, "workspace-home-create@example.com")

	homeRequest := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
	home := serve(homeRequest)
	if home.Code != http.StatusOK || !strings.Contains(home.Body.String(), `data-create-space-dialog`) {
		t.Fatalf("home must present a space creation dialog: %d", home.Code)
	}

	formRequest := httptest.NewRequest(http.MethodPost, "/spaces", strings.NewReader(url.Values{"name": {"示例空间 & 研发"}}.Encode()))
	formRequest.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	formRequest.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
	created := serve(formRequest)
	if created.Code != http.StatusSeeOther {
		t.Fatalf("workspace dialog submit returned %d: %s", created.Code, created.Body.String())
	}
	editorURL, err := url.Parse(created.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	path := strings.Split(strings.Trim(editorURL.Path, "/"), "/")
	if len(path) != 3 || path[0] != "r" {
		t.Fatalf("expected redirect to the example document, got %s", editorURL)
	}
	spaceID, documentID := path[1], path[2]

	treeRequest := httptest.NewRequest(http.MethodGet, "/api/spaces/"+spaceID+"/tree", nil)
	treeRequest.Header.Set("Authorization", "Bearer "+token)
	treeResponse := serve(treeRequest)
	tree := decodeJSONResultData[[]struct {
		DocumentID string
		Title      string
	}](t, treeResponse.Body.Bytes())
	if len(tree) != 1 || tree[0].DocumentID != documentID || tree[0].Title != "示例文档" {
		t.Fatalf("space should start with exactly one example document: %+v", tree)
	}

	docRequest := httptest.NewRequest(http.MethodGet, "/api/docs/"+documentID, nil)
	docRequest.Header.Set("Authorization", "Bearer "+token)
	document := decodeJSONResultData[struct {
		ContentMD string
		Version   int
	}](t, serve(docRequest).Body.Bytes())
	if !strings.Contains(document.ContentMD, "# 欢迎使用 PlainDoc") || document.Version != 1 {
		t.Fatalf("example document content was not initialized: %+v", document)
	}
	coverPath := "/space-covers/" + spaceID
	homeRequest = httptest.NewRequest(http.MethodGet, "/", nil)
	homeRequest.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
	homeHTML := serve(homeRequest).Body.String()
	if !strings.Contains(homeHTML, `src="`+coverPath+`"`) ||
		!strings.Contains(strings.Join(strings.Fields(homeHTML), " "), `href="/r/`+spaceID+`" aria-label="进入空间`) {
		t.Fatalf("homepage should show the default cover and open its space in the current tab")
	}

	coverRequest := httptest.NewRequest(http.MethodGet, coverPath, nil)
	if anonymous := serve(coverRequest); anonymous.Code != http.StatusNotFound {
		t.Fatalf("anonymous request returned %d for a members-only cover", anonymous.Code)
	}
	coverRequest = httptest.NewRequest(http.MethodGet, coverPath, nil)
	coverRequest.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
	cover := serve(coverRequest)
	if cover.Code != http.StatusOK || !strings.HasPrefix(cover.Header().Get("Content-Type"), "image/svg+xml") ||
		!strings.Contains(cover.Body.String(), `<title id="cover-title">示例空间 &amp; 研发</title>`) ||
		!strings.Contains(cover.Body.String(), `<tspan`) {
		t.Fatalf("space owner should receive the escaped title image: %d %s", cover.Code, cover.Body.String())
	}

	readerRequest := httptest.NewRequest(http.MethodGet, "/r/"+spaceID+"/"+documentID, nil)
	readerRequest.AddCookie(&http.Cookie{Name: "accessToken", Value: token})
	reader := serve(readerRequest)
	if reader.Code != http.StatusOK || !strings.Contains(reader.Body.String(), `class="reader-home-link" href="/"`) {
		t.Fatalf("fallback reader should provide a homepage link: %d", reader.Code)
	}
	if !strings.Contains(reader.Body.String(), "/editor/"+spaceID+"/"+documentID+"?from=reader") {
		t.Fatal("space owner must be able to reopen the example document for editing from the reader")
	}

	var revisions int64
	if err := database.ORM.Model(&models.DocumentRevision{}).Where("document_id = ?", documentID).Count(&revisions).Error; err != nil || revisions != 1 {
		t.Fatalf("expected one initial revision, got %d: %v", revisions, err)
	}
}
