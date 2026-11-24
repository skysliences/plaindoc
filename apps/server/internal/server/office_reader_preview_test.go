package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/xuri/excelize/v2"
)

func TestRouter_ReaderPreviewsStoredOfficeWhenOnlyOfficeDisabled(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "local-office-preview@example.com")
	spaceID := "01h1localofficepreview000001"
	seedOnlyOfficeEnabledConfig(t, database)
	seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")

	const wordText = "局域网 Word 报告"
	const xlsxText = "局域网 Excel 预算"
	book := excelize.NewFile()
	t.Cleanup(func() { _ = book.Close() })
	if err := book.SetCellValue("Sheet1", "A1", xlsxText); err != nil {
		t.Fatal(err)
	}
	bookFile, err := book.WriteToBuffer()
	if err != nil {
		t.Fatal(err)
	}
	body, contentType := buildWorkspaceImportMultipartBody(t, "", map[string][]byte{
		"office-source.docx": buildMinimalImportDOCX(t, wordText),
		"office-source.xlsx": bookFile.Bytes(),
	})
	importRequest := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
	importRequest.Header.Set("Authorization", "Bearer "+token)
	importRequest.Header.Set("Content-Type", contentType)
	if response := serve(importRequest); response.Code != http.StatusOK {
		t.Fatalf("import failed with status %d: %s", response.Code, response.Body.String())
	}
	if err := database.ORM.Where("config_key IN ?", []string{service.SystemConfigKeyOnlyOfficeIntegration, service.SystemConfigKeyOnlyOfficeLegacy}).
		Delete(&models.SystemConfig{}).Error; err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		format    models.DocumentFormat
		text      string
		container string
	}{
		{models.DocumentFormatDOCX, wordText, "office-docx-reader"},
		{models.DocumentFormatXLSX, xlsxText, "office-xlsx-reader"},
	}
	for _, test := range cases {
		t.Run(string(test.format), func(t *testing.T) {
			var document models.Document
			if err := database.ORM.Where(models.DocumentColumns.Format+" = ?", test.format).Take(&document).Error; err != nil {
				t.Fatal(err)
			}
			var node models.Node
			if err := database.ORM.Where(models.NodeColumns.NodeID+" = ?", document.NodeID).Take(&node).Error; err != nil {
				t.Fatal(err)
			}
			routeKey := document.DocumentID
			if node.ReaderSlug != nil {
				routeKey = *node.ReaderSlug
			}
			route := "/r/" + spaceID + "/" + routeKey
			anonymous := serve(httptest.NewRequest(http.MethodGet, route, nil))
			if strings.Contains(anonymous.Body.String(), test.text) {
				t.Fatal("anonymous reader received private Office document")
			}
			request := httptest.NewRequest(http.MethodGet, route, nil)
			request.Header.Set("Authorization", "Bearer "+token)
			owner := serve(request)
			html := owner.Body.String()
			if owner.Code != http.StatusOK || !strings.Contains(html, `class="`+test.container) || !strings.Contains(html, test.text) {
				t.Fatalf("expected local Office preview, status %d: %s", owner.Code, html)
			}
			if strings.Contains(html, `"editUrl"`) {
				t.Fatal("local preview exposed an unavailable edit link")
			}
			var persisted models.Document
			if err := database.ORM.Where(models.DocumentColumns.DocumentID+" = ?", document.DocumentID).Take(&persisted).Error; err != nil {
				t.Fatal(err)
			}
			if persisted.RenderStatus != models.DocumentRenderStatusSuccess || !strings.Contains(persisted.ContentMD, test.text) ||
				persisted.SourceBlobID == nil || document.SourceBlobID == nil || *persisted.SourceBlobID != *document.SourceBlobID ||
				persisted.ContentVersion != document.ContentVersion {
				t.Fatal("local preview did not preserve the original Office source and version")
			}
		})
	}
}
