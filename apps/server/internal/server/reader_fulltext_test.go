package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_ReaderFullTextSearchRespectsSiteSetting(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	_, _, token := registerAccessUser(t, serve, "reader-fulltext@example.com")

	request := func(method, path, body string) string {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		rec := serve(req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s: expected 200, got %d: %s", method, path, rec.Code, rec.Body.String())
		}
		return rec.Body.String()
	}

	created := decodeJSONResultData[struct{ ID, ExampleDocumentID string }](
		t, []byte(request(http.MethodPost, "/api/spaces", `{"name":"站内搜索测试空间"}`)),
	)
	readerPath := "/r/" + created.ID + "/" + created.ExampleDocumentID
	if body := request(http.MethodGet, readerPath, ""); !strings.Contains(body, `action="/search"`) {
		t.Fatal("reader did not render the default search entry")
	}
	if body := request(http.MethodGet, "/search?q=bugodoc", ""); !strings.Contains(body, ">示例文档</a>") {
		t.Fatal("default search did not find the example document by its body")
	}

	if err := database.ORM.Create(&models.SystemConfig{
		ConfigKey:       "search",
		ConfigValueJSON: `{"enabled":false,"activeProvider":"database","fallbackPolicy":"degrade_to_database"}`,
		Version:         1,
	}).Error; err != nil {
		t.Fatal(err)
	}
	if body := request(http.MethodGet, readerPath, ""); strings.Contains(body, `action="/search"`) {
		t.Fatal("reader shows the search entry after search was disabled")
	}
	if body := request(http.MethodGet, "/search?q=bugodoc", ""); !strings.Contains(body, "当前未开启全文检索") {
		t.Fatal("search page did not respect the disabled setting")
	}
}
