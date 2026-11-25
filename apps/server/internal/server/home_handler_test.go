package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_Home_AnonymousUsesPublicCacheAndVisibility(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	ownerUserID, _, _ := registerAccessUser(t, serve, "home-owner@example.com")
	categoryID := "01homecate000000000000000001"
	seedHomepageCategory(t, database, categoryID, "产品文档", false)

	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeanonspace00000000000001",
		Name:         "Public Space A",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   categoryID,
		CategoryName: "产品文档",
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeanonspace00000000000002",
		Name:         "Public Space B",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   models.DefaultSpaceCategoryID,
		CategoryName: models.DefaultSpaceCategoryName,
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeanonspace00000000000003",
		Name:         "Authenticated Hidden",
		OwnerUserID:  ownerUserID,
		Visibility:   "authenticated",
		CategoryID:   categoryID,
		CategoryName: "产品文档",
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeanonspace00000000000004",
		Name:         "Member Hidden",
		OwnerUserID:  ownerUserID,
		Visibility:   "member",
		CategoryID:   categoryID,
		CategoryName: "产品文档",
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeanonspace00000000000005",
		Name:         "Public With AuthDoc Hidden",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   categoryID,
		CategoryName: "产品文档",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeanonspace00000000000001",
		NodeID:     "01homeanondocnode000000000001",
		DocumentID: "01homeanondocument000000000001",
		Title:      "Public Space A - Doc",
		Visibility: "public",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeanonspace00000000000002",
		NodeID:     "01homeanondocnode000000000002",
		DocumentID: "01homeanondocument000000000002",
		Title:      "Public Space B - Doc",
		Visibility: "public",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeanonspace00000000000003",
		NodeID:     "01homeanondocnode000000000003",
		DocumentID: "01homeanondocument000000000003",
		Title:      "Authenticated Hidden - Doc",
		Visibility: "authenticated",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeanonspace00000000000004",
		NodeID:     "01homeanondocnode000000000004",
		DocumentID: "01homeanondocument000000000004",
		Title:      "Member Hidden - Doc",
		Visibility: "member",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeanonspace00000000000005",
		NodeID:     "01homeanondocnode000000000005",
		DocumentID: "01homeanondocument000000000005",
		Title:      "Public With AuthDoc Hidden - Doc",
		Visibility: "authenticated",
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := serve(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("expected text/html content type, got %q", rec.Header().Get("Content-Type"))
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=60, s-maxage=300, stale-while-revalidate=60" {
		t.Fatalf("unexpected cache-control header: %q", got)
	}
	varyHeader := rec.Header().Get("Vary")
	if !strings.Contains(varyHeader, "Authorization") || !strings.Contains(varyHeader, "Cookie") {
		t.Fatalf("expected Vary include Authorization/Cookie, got %q", varyHeader)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `class="yt-header-search-form"`) {
		t.Fatal("expected the anonymous homepage to show search with the default settings")
	}
	if !strings.Contains(body, `class="yt-signin-button"`) || !strings.Contains(body, ">登录<") {
		t.Fatalf("expected anonymous nav has login button, body=%s", body)
	}
	if !strings.Contains(body, `class="yt-register-button"`) || !strings.Contains(body, ">注册<") {
		t.Fatalf("expected anonymous nav has register button, body=%s", body)
	}
	if !strings.Contains(body, `href="http://localhost:5173/login?redirect=http%3A%2F%2Fexample.com%2F"`) {
		t.Fatalf("expected login button links web auth page with redirect, body=%s", body)
	}
	if !strings.Contains(body, `href="http://localhost:5173/register?redirect=http%3A%2F%2Fexample.com%2F"`) {
		t.Fatalf("expected register button links web auth page with redirect, body=%s", body)
	}
	if !strings.Contains(body, "Public Space A") || !strings.Contains(body, "Public Space B") {
		t.Fatalf("expected anonymous page contains public spaces, body=%s", body)
	}
	if strings.Contains(body, "Authenticated Hidden") || strings.Contains(body, "Member Hidden") || strings.Contains(body, "Public With AuthDoc Hidden") {
		t.Fatalf("expected anonymous page hide non-public spaces, body=%s", body)
	}
	if !strings.Contains(body, "/explore/"+categoryID) {
		t.Fatalf("expected category link in body, body=%s", body)
	}
	if !strings.Contains(body, `class="yt-sidebar-item is-active" href="/"`) {
		t.Fatalf("expected homepage sidebar has active all-category link, body=%s", body)
	}
	allIndex := strings.Index(body, `href="/">`)
	defaultIndex := strings.Index(body, `/explore/`+models.DefaultSpaceCategoryID)
	if allIndex < 0 || defaultIndex < 0 || allIndex >= defaultIndex {
		t.Fatalf("expected all-category link keeps first position, body=%s", body)
	}
}

func TestRouter_Explore_LoggedInUsesNoStoreAndCategoryFilter(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	ownerUserID, _, ownerToken := registerAccessUser(t, serve, "explore-owner@example.com")
	viewerUserID, _, viewerToken := registerAccessUser(t, serve, "explore-viewer@example.com")
	categoryID := "01homecate000000000000000010"
	emptyCategoryID := "01homecate000000000000000011"
	seedHomepageCategory(t, database, categoryID, "技术文档", false)
	seedHomepageCategory(t, database, emptyCategoryID, "空分类", false)

	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeexplorespace000000000001",
		Name:         "Public In Category",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   categoryID,
		CategoryName: "技术文档",
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeexplorespace000000000002",
		Name:         "Authenticated In Category",
		OwnerUserID:  ownerUserID,
		Visibility:   "authenticated",
		CategoryID:   categoryID,
		CategoryName: "技术文档",
	})
	memberSpaceID := "01homeexplorespace000000000003"
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      memberSpaceID,
		Name:         "Member In Category",
		OwnerUserID:  ownerUserID,
		Visibility:   "member",
		CategoryID:   categoryID,
		CategoryName: "技术文档",
	})
	seedHomepageSpaceMember(t, database, memberSpaceID, viewerUserID, "reader")
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homeexplorespace000000000004",
		Name:         "Public In Other Category",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   models.DefaultSpaceCategoryID,
		CategoryName: models.DefaultSpaceCategoryName,
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeexplorespace000000000001",
		NodeID:     "01homeexploredocnode000000000001",
		DocumentID: "01homeexploredocument000000000001",
		Title:      "Public In Category - Doc",
		Visibility: "public",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeexplorespace000000000002",
		NodeID:     "01homeexploredocnode000000000002",
		DocumentID: "01homeexploredocument000000000002",
		Title:      "Authenticated In Category - Doc",
		Visibility: "authenticated",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeexplorespace000000000003",
		NodeID:     "01homeexploredocnode000000000003",
		DocumentID: "01homeexploredocument000000000003",
		Title:      "Member In Category - Doc",
		Visibility: "member",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homeexplorespace000000000004",
		NodeID:     "01homeexploredocnode000000000004",
		DocumentID: "01homeexploredocument000000000004",
		Title:      "Public In Other Category - Doc",
		Visibility: "public",
	})

	req := httptest.NewRequest(http.MethodGet, "/explore/"+categoryID, nil)
	req.Header.Set("Authorization", "Bearer "+viewerToken)
	rec := serve(req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Cache-Control"); got != "private, no-store, max-age=0" {
		t.Fatalf("unexpected cache-control for logged-in request: %q", got)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Test User") || strings.Contains(body, "我的空间") || strings.Contains(body, "后台管理") || !strings.Contains(body, "退出登录") {
		t.Fatalf("expected logged-in nav dropdown items, body=%s", body)
	}
	if !strings.Contains(body, `href="/admin/profile" class="yt-user-menu-item">个人设置</a>`) {
		t.Fatal("expected a personal settings link for a logged-in normal user")
	}
	if !strings.Contains(body, "Public In Category") ||
		!strings.Contains(body, "Authenticated In Category") ||
		!strings.Contains(body, "Member In Category") {
		t.Fatalf("expected category page contains visible category spaces, body=%s", body)
	}
	if strings.Contains(body, "Public In Other Category") {
		t.Fatalf("expected category page filter by category id, body=%s", body)
	}

	if strings.Contains(body, "data-space-manage-url") {
		t.Fatal("space management triggers must be hidden from non-owners")
	}
	for _, path := range []string{"/", "/explore/" + categoryID} {
		ownerReq := httptest.NewRequest(http.MethodGet, path, nil)
		ownerReq.Header.Set("Authorization", "Bearer "+ownerToken)
		ownerRec := serve(ownerReq)
		ownerBody := ownerRec.Body.String()
		if ownerRec.Code != http.StatusOK ||
			!strings.Contains(ownerBody, `class="yt-card-manage" data-space-manage-url="`) ||
			!strings.Contains(ownerBody, `?space=01homeexplorespace000000000001&amp;manage=dialog`) ||
			!strings.Contains(ownerBody, `<dialog id="space-management-dialog"`) {
			t.Fatalf("expected an owner-only trigger and management dialog on %s, status=%d", path, ownerRec.Code)
		}
	}

	emptyReq := httptest.NewRequest(http.MethodGet, "/explore/"+emptyCategoryID, nil)
	emptyReq.Header.Set("Authorization", "Bearer "+viewerToken)
	emptyRec := serve(emptyReq)
	if emptyRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for empty category, got %d body=%s", emptyRec.Code, emptyRec.Body.String())
	}
	if !strings.Contains(emptyRec.Body.String(), "该分类下暂无可见空间") {
		t.Fatalf("expected empty category message, body=%s", emptyRec.Body.String())
	}

	allReq := httptest.NewRequest(http.MethodGet, "/explore/all", nil)
	allReq.Header.Set("Authorization", "Bearer "+viewerToken)
	allRec := serve(allReq)
	if allRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for explore all, got %d body=%s", allRec.Code, allRec.Body.String())
	}
	allBody := allRec.Body.String()
	if !strings.Contains(allBody, `class="yt-sidebar-item is-active" href="/explore/all"`) {
		t.Fatalf("expected explore all sidebar category active, body=%s", allBody)
	}
	exploreAllIndex := strings.Index(allBody, `href="/explore/all"`)
	exploreDefaultIndex := strings.Index(allBody, `/explore/`+models.DefaultSpaceCategoryID)
	if exploreAllIndex < 0 || exploreDefaultIndex < 0 || exploreAllIndex >= exploreDefaultIndex {
		t.Fatalf("expected explore all sidebar category keeps first position, body=%s", allBody)
	}
	if !strings.Contains(allBody, "Public In Category") ||
		!strings.Contains(allBody, "Authenticated In Category") ||
		!strings.Contains(allBody, "Member In Category") ||
		!strings.Contains(allBody, "Public In Other Category") {
		t.Fatalf("expected explore all page contains all visible spaces, body=%s", allBody)
	}
}

func TestRouter_HomeAndExplore_PlatformAdminSeesActivePrivateSpaces(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer database.Close()

	ownerID, _, _ := registerAccessUser(t, serve, "home-private-owner@example.com")
	platformAdminID, _, platformAdminToken := registerAccessUser(t, serve, "home-platform-admin@example.com")
	spaceAdminID, _, spaceAdminToken := registerAccessUser(t, serve, "home-space-admin@example.com")
	if err := database.ORM.Create(&[]models.UserAdminRole{
		{UserID: platformAdminID, Role: models.AdminRolePlatformAdmin},
		{UserID: spaceAdminID, Role: models.AdminRoleSpaceAdmin},
	}).Error; err != nil {
		t.Fatalf("insert admin roles: %v", err)
	}

	const categoryID = "01homecate000000000000000020"
	const otherCategoryID = "01homecate000000000000000021"
	const privateSpaceID = "01homeprivatespace0000000001"
	const emptySpaceID = "01homeprivatespace0000000002"
	const bannedSpaceID = "01homeprivatespace0000000003"
	const deletedSpaceID = "01homeprivatespace0000000004"
	seedHomepageCategory(t, database, categoryID, "私有空间分类", false)
	seedHomepageCategory(t, database, otherCategoryID, "另一分类", false)
	for _, space := range []homepageSeedSpaceInput{
		{SpaceID: privateSpaceID, Name: "仅成员可见的空间", CategoryID: categoryID, CategoryName: "私有空间分类"},
		{SpaceID: emptySpaceID, Name: "暂未添加文档的私有空间", CategoryID: otherCategoryID, CategoryName: "另一分类"},
		{SpaceID: bannedSpaceID, Name: "已封禁的私有空间", CategoryID: categoryID, CategoryName: "私有空间分类"},
		{SpaceID: deletedSpaceID, Name: "已删除的私有空间", CategoryID: categoryID, CategoryName: "私有空间分类"},
	} {
		space.OwnerUserID = ownerID
		space.Visibility = string(models.VisibilityMember)
		seedHomepageSpace(t, database, space)
	}
	for spaceID, status := range map[string]models.EntityStatus{
		bannedSpaceID: models.EntityStatusBanned, deletedSpaceID: models.EntityStatusDeleted,
	} {
		if err := database.ORM.Model(&models.Space{}).
			Where(models.SpaceColumns.SpaceID+" = ?", spaceID).
			Update(models.SpaceColumns.Status, status).Error; err != nil {
			t.Fatalf("update space status: %v", err)
		}
	}
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    privateSpaceID,
		NodeID:     "01homeprivatenode00000000001",
		DocumentID: "01homeprivatedoc000000000001",
		Title:      "仅成员可见的文档",
		Visibility: string(models.VisibilityMember),
	})

	for _, tc := range []struct {
		name        string
		path        string
		token       string
		wantPrivate bool
		wantEmpty   bool
	}{
		{name: "platform home", path: "/", token: platformAdminToken, wantPrivate: true, wantEmpty: true},
		{name: "platform category", path: "/explore/" + categoryID, token: platformAdminToken, wantPrivate: true},
		{name: "unscoped space admin", path: "/", token: spaceAdminToken},
		{name: "anonymous", path: "/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			rec := serve(req)
			if rec.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", rec.Code)
			}
			if tc.token != "" && rec.Header().Get("Cache-Control") != "private, no-store, max-age=0" {
				t.Fatalf("expected private authenticated response, got %q", rec.Header().Get("Cache-Control"))
			}
			body := rec.Body.String()
			if strings.Contains(body, `href="/r/`+privateSpaceID+`"`) != tc.wantPrivate ||
				strings.Contains(body, `href="/r/`+emptySpaceID+`"`) != tc.wantEmpty {
				t.Fatalf("unexpected visibility for private or empty space")
			}
			if strings.Contains(body, bannedSpaceID) || strings.Contains(body, deletedSpaceID) ||
				strings.Contains(body, "data-space-manage-url") {
				t.Fatal("expected banned and deleted spaces and owner-only management to stay hidden")
			}
		})
	}
}

func TestRouter_HomeSearchEnabled_ShowsSearchBoxAndResults(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	ownerUserID, _, _ := registerAccessUser(t, serve, "home-search-owner@example.com")
	seedHomepageSearchConfig(t, database)

	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homesearchspace000000000001",
		Name:         "公开检索空间",
		OwnerUserID:  ownerUserID,
		Visibility:   "public",
		CategoryID:   models.DefaultSpaceCategoryID,
		CategoryName: models.DefaultSpaceCategoryName,
	})
	seedHomepageSpace(t, database, homepageSeedSpaceInput{
		SpaceID:      "01homesearchspace000000000002",
		Name:         "仅成员空间",
		OwnerUserID:  ownerUserID,
		Visibility:   "member",
		CategoryID:   models.DefaultSpaceCategoryID,
		CategoryName: models.DefaultSpaceCategoryName,
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homesearchspace000000000001",
		NodeID:     "01homesearchdocnode000000000001",
		DocumentID: "01homesearchdocument00000000001",
		Title:      "检索命中文档",
		Format:     "markdown",
		Visibility: "public",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homesearchspace000000000002",
		NodeID:     "01homesearchdocnode000000000002",
		DocumentID: "01homesearchdocument00000000002",
		Title:      "不应被匿名检索命中",
		Format:     "markdown",
		Visibility: "member",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homesearchspace000000000001",
		NodeID:     "01homesearchdocnode000000000003",
		DocumentID: "01homesearchdocument00000000003",
		Title:      "命中办公表格",
		Format:     "xlsx",
		Visibility: "public",
	})
	seedHomepageDocument(t, database, homepageSeedDocumentInput{
		SpaceID:    "01homesearchspace000000000001",
		NodeID:     "01homesearchdocnode000000000004",
		DocumentID: "01homesearchdocument00000000004",
		Title:      "第二个命中文档",
		Format:     "markdown",
		Visibility: "public",
	})

	homeReq := httptest.NewRequest(http.MethodGet, "/", nil)
	homeRec := serve(homeReq)
	if homeRec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", homeRec.Code, homeRec.Body.String())
	}
	homeBody := homeRec.Body.String()
	if !strings.Contains(homeBody, `class="yt-header-search-form"`) {
		t.Fatalf("expected homepage renders search form after enabling search config, body=%s", homeBody)
	}
	if !strings.Contains(homeBody, `action="/search"`) {
		t.Fatalf("expected homepage search form action to /search, body=%s", homeBody)
	}

	searchReq := httptest.NewRequest(http.MethodGet, "/search?q=命中", nil)
	searchRec := serve(searchReq)
	if searchRec.Code != http.StatusOK {
		t.Fatalf("expected status 200 for /search, got %d body=%s", searchRec.Code, searchRec.Body.String())
	}
	searchBody := searchRec.Body.String()
	if !strings.Contains(searchBody, `检索<mark class="yt-search-highlight">命中</mark>文档`) {
		t.Fatalf("expected search page contains matched document title, body=%s", searchBody)
	}
	if !strings.Contains(searchBody, "/r/01homesearchspace000000000001/01homesearchdocument00000000001") {
		t.Fatalf("expected search result links reader page, body=%s", searchBody)
	}
	if strings.Contains(searchBody, "/r/01homesearchspace000000000002/01homesearchdocument00000000002") {
		t.Fatalf("expected anonymous search result excludes member-only document, body=%s", searchBody)
	}
	if strings.Contains(searchBody, "/r/01homesearchspace000000000001/01homesearchdocument00000000003") {
		t.Fatalf("expected homepage search excludes office document, body=%s", searchBody)
	}

	const firstURL = "/search?page=1&pageSize=1&q=%E5%91%BD%E4%B8%AD"
	const secondURL = "/search?page=2&pageSize=1&q=%E5%91%BD%E4%B8%AD"
	firstPage := serve(httptest.NewRequest(http.MethodGet, firstURL, nil))
	secondPage := serve(httptest.NewRequest(http.MethodGet, secondURL, nil))
	firstBody, secondBody := firstPage.Body.String(), secondPage.Body.String()
	if firstPage.Code != http.StatusOK || secondPage.Code != http.StatusOK ||
		strings.Count(firstBody, `class="yt-search-card-link"`) != 1 || strings.Count(secondBody, `class="yt-search-card-link"`) != 1 {
		t.Fatalf("expected one search result per page, got statuses %d, %d", firstPage.Code, secondPage.Code)
	}
	if !strings.Contains(firstBody, `rel="next" href="`+strings.ReplaceAll(secondURL, "&", "&amp;")+`"`) ||
		!strings.Contains(secondBody, `rel="prev" href="`+strings.ReplaceAll(firstURL, "&", "&amp;")+`"`) {
		t.Fatal("expected search navigation to preserve the keyword and page size")
	}
	outOfRange := serve(httptest.NewRequest(http.MethodGet, "/search?q=命中&pageSize=1&page=3", nil))
	if outOfRange.Code != http.StatusFound || outOfRange.Header().Get("Location") != secondURL {
		t.Fatal("expected an out-of-range request to redirect to the last search page")
	}
}

func TestRouter_HomePages_RenderFooterWithSiteFilingNumber(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	seedHomepageSiteConfig(t, database, "Acme Docs", "沪ICP备2026000001号", "https://beian.miit.gov.cn/")

	paths := []string{"/", "/explore/all", "/search"}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := serve(req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}

		body := rec.Body.String()
		_, afterFooterStart, hasFooter := strings.Cut(body, `<footer class="yt-footer"`)
		footer, _, hasFooterEnd := strings.Cut(afterFooterStart, "</footer>")
		if !hasFooter || !hasFooterEnd {
			t.Fatalf("expected footer with filing information on %s", path)
		}
		if strings.Contains(footer, "Acme Docs") || strings.Contains(footer, "PlainDoc") ||
			strings.Contains(footer, "一个适合中小团队文档在线管理系统") {
			t.Fatalf("expected no branding or description in footer on %s, footer=%s", path, footer)
		}
		if !strings.Contains(footer, "沪ICP备2026000001号") {
			t.Fatalf("expected footer contains filing number on %s, footer=%s", path, footer)
		}
		if !strings.Contains(footer, "https://beian.miit.gov.cn/") {
			t.Fatalf("expected footer filing link exists on %s, footer=%s", path, footer)
		}
	}
}

func TestRouter_HomePages_OmitFooterWhenNoFilingNumber(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	seedHomepageSiteConfig(t, database, "Acme Docs", "", "https://beian.miit.gov.cn/")

	paths := []string{"/", "/explore/all", "/search"}
	for _, path := range paths {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := serve(req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected status 200 for %s, got %d body=%s", path, rec.Code, rec.Body.String())
		}

		if strings.Contains(rec.Body.String(), `<footer class="yt-footer"`) {
			t.Fatalf("expected no footer without a filing number on %s", path)
		}
	}
}

func TestRouter_HomeLogoutRedirectsAndClearsCookie(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	defer func() {
		_ = database.Close()
	}()

	_, _, accessToken := registerAccessUser(t, serve, "home-logout@example.com")

	req := httptest.NewRequest(http.MethodPost, "/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  "accessToken",
		Value: accessToken,
		Path:  "/",
	})
	rec := serve(req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("expected status 303, got %d body=%s", rec.Code, rec.Body.String())
	}
	if location := rec.Header().Get("Location"); location != "/" {
		t.Fatalf("expected redirect to /, got %q", location)
	}
	setCookieHeader := rec.Header().Values("Set-Cookie")
	if len(setCookieHeader) == 0 {
		t.Fatal("expected set-cookie headers for clearing session cookies")
	}
	joined := strings.Join(setCookieHeader, "\n")
	if !strings.Contains(joined, "accessToken=") || !strings.Contains(joined, "refreshToken=") {
		t.Fatalf("expected clear accessToken/refreshToken cookies, got %v", setCookieHeader)
	}
}

type homepageSeedSpaceInput struct {
	SpaceID      string
	Name         string
	OwnerUserID  string
	Visibility   string
	CategoryID   string
	CategoryName string
}

type homepageSeedDocumentInput struct {
	SpaceID    string
	NodeID     string
	DocumentID string
	Title      string
	Format     string
	Visibility string
}

func seedHomepageCategory(
	t *testing.T,
	database *storage.Database,
	categoryID string,
	name string,
	isDefault bool,
) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := database.ORM.Table("space_categories").Create(map[string]any{
		"category_id": categoryID,
		"name":        name,
		"is_default":  isDefault,
		"created_at":  now,
		"updated_at":  now,
	}).Error; err != nil {
		t.Fatalf("insert space category failed: %v", err)
	}
}

func seedHomepageSpace(t *testing.T, database *storage.Database, input homepageSeedSpaceInput) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := database.ORM.Table("spaces").Create(map[string]any{
		"space_id":      input.SpaceID,
		"name":          input.Name,
		"description":   input.Name + " description",
		"category_id":   input.CategoryID,
		"category":      input.CategoryName,
		"owner_user_id": input.OwnerUserID,
		"visibility":    input.Visibility,
		"cover_url":     "https://example.com/" + input.SpaceID + ".webp",
		"status":        "active",
		"created_at":    now,
		"updated_at":    now,
	}).Error; err != nil {
		t.Fatalf("insert homepage space failed: %v", err)
	}
}

func seedHomepageSpaceMember(
	t *testing.T,
	database *storage.Database,
	spaceID string,
	userID string,
	role string,
) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := database.ORM.Table("space_members").Create(map[string]any{
		"space_id":   spaceID,
		"user_id":    userID,
		"role":       role,
		"created_at": now,
		"updated_at": now,
	}).Error; err != nil {
		t.Fatalf("insert homepage space member failed: %v", err)
	}
}

func seedHomepageDocument(t *testing.T, database *storage.Database, input homepageSeedDocumentInput) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	documentFormat := input.Format
	if strings.TrimSpace(documentFormat) == "" {
		documentFormat = "markdown"
	}
	if err := database.ORM.Table("nodes").Create(map[string]any{
		"node_id":        input.NodeID,
		"space_id":       input.SpaceID,
		"parent_node_id": nil,
		"type":           "doc",
		"title":          input.Title,
		"sort":           1,
		"created_at":     now,
		"updated_at":     now,
	}).Error; err != nil {
		t.Fatalf("insert homepage doc node failed: %v", err)
	}

	if err := database.ORM.Table("documents").Create(map[string]any{
		"document_id": input.DocumentID,
		"node_id":     input.NodeID,
		"theme_id":    "default",
		"format":      documentFormat,
		"visibility":  input.Visibility,
		"status":      "active",
		"title":       input.Title,
		"content_md":  "# " + input.Title,
		"version":     1,
		"created_at":  now,
		"updated_at":  now,
	}).Error; err != nil {
		t.Fatalf("insert homepage document failed: %v", err)
	}
}

func seedHomepageSearchConfig(t *testing.T, database *storage.Database) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := database.ORM.Table("system_configs").Create(map[string]any{
		"config_key":         "search",
		"config_value_json":  `{"enabled":true,"activeProvider":"database","fallbackPolicy":"degrade_to_bleve","analysis":{"activeAnalyzer":"simple","analyzers":{"simple":{"enabled":true},"jieba":{"enabled":false,"mode":"search","hmm":true,"stopwordsEnabled":false,"dictSource":"db","dictVersion":"default"}}}}`,
		"version":            1,
		"updated_by_user_id": nil,
		"created_at":         now,
		"updated_at":         now,
	}).Error; err != nil {
		t.Fatalf("insert homepage search config failed: %v", err)
	}
}

func seedHomepageSiteConfig(
	t *testing.T,
	database *storage.Database,
	siteName string,
	filingNumber string,
	filingLink string,
) {
	t.Helper()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := database.ORM.Table("system_configs").Create(map[string]any{
		"config_key":         "site",
		"config_value_json":  `{"allowRegistration":true,"defaultSpaceVisibility":"member","siteName":"` + siteName + `","filingNumber":"` + filingNumber + `","filingLink":"` + filingLink + `"}`,
		"version":            1,
		"updated_by_user_id": nil,
		"created_at":         now,
		"updated_at":         now,
	}).Error; err != nil {
		t.Fatalf("insert homepage site config failed: %v", err)
	}
}
