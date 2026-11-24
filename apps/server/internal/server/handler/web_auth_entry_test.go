package handler

import (
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestSameOriginLoginReturnsToRequestedPage(t *testing.T) {
	for _, origin := range []string{"http://192.168.1.8:8081", "https://bugodoc.example"} {
		request := httptest.NewRequest("GET", origin+"/r/team/doc?tab=files", nil)
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = request
		entry, err := url.Parse(buildWebAuthEntryURL(c, "", "/login"))
		if err != nil || entry.IsAbs() || entry.Host != "" || entry.Path != "/login" || entry.Query().Get("redirect") != "/r/team/doc?tab=files" {
			t.Fatalf("unexpected login link for %s: %v (%v)", origin, entry, err)
		}
	}
}
