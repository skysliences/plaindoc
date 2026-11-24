package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_ImportDocuments_SkipsHiddenFilesAndDirectories(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	ownerID, _, token := registerAccessUser(t, serve, "hidden-import@example.com")

	tests := []struct {
		name         string
		build        func(*testing.T) (*bytes.Buffer, string)
		wantTitles   []string
		wantSkipped  int
		wantRejected bool
	}{
		{
			name: "files",
			build: func(t *testing.T) (*bytes.Buffer, string) {
				return buildWorkspaceImportMultipartBodyWithOptions(t, "", map[string][]byte{
					"report.md":       []byte("# .draft"),
					".DS_Store":       []byte("hidden"),
					".broken.zip":     []byte("this archive must not be opened"),
					".parent/leaf.md": []byte("hidden"),
				}, map[string]string{"autoExtractTitle": "true"})
			},
			wantTitles:  []string{"report"},
			wantSkipped: 3,
		},
		{
			name: "folder",
			build: func(t *testing.T) (*bytes.Buffer, string) {
				return buildHiddenImportFolderMultipart(t,
					`{"paths":["project/README.md","project/._README.md","project/.git/cache.txt","project/source/report.txt"],"directories":["project/.private/deeper","project/empty"]}`,
					"project/README.md", "project/._README.md", "project/.git/cache.txt", "project/source/report.txt",
				)
			},
			wantTitles:  []string{"README.md", "empty", "project", "report.txt", "source"},
			wantSkipped: 5,
		},
		{
			name: "zip",
			build: func(t *testing.T) (*bytes.Buffer, string) {
				archive := buildWorkspaceImportZIP(t, map[string][]byte{
					"./directory/notes.md": []byte("# Normal"),
					"directory/._notes.md": []byte("hidden"),
					"directory/.DS_Store":  []byte("hidden"),
					".git/":                nil,
					".git/cache.txt":       []byte("hidden"),
					".assets/image.png":    []byte("hidden"),
					".nested.zip":          []byte("this archive must not be opened"),
				})
				return buildWorkspaceImportMultipartBody(t, "", map[string][]byte{"source.zip": archive})
			},
			wantTitles:  []string{"notes", "source"},
			wantSkipped: 6,
		},
		{
			name: "hidden-root",
			build: func(t *testing.T) (*bytes.Buffer, string) {
				return buildHiddenImportFolderMultipart(t,
					`{"paths":[".config/settings.md"],"directories":[".config/nested"]}`,
					".config/settings.md",
				)
			},
			wantSkipped: 3,
		},
		{
			name: "invalid-zip-path",
			build: func(t *testing.T) (*bytes.Buffer, string) {
				archive := buildWorkspaceImportZIP(t, map[string][]byte{"../.git/private.md": []byte("invalid")})
				return buildWorkspaceImportMultipartBody(t, "", map[string][]byte{"invalid.zip": archive})
			},
			wantRejected: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			spaceID := "hidden-import-" + test.name
			seedSpaceForWorkspaceCreateNode(t, database, ownerID, spaceID, "member")
			body, contentType := test.build(t)
			req := httptest.NewRequest(http.MethodPost, "/api/spaces/"+spaceID+"/imports", body)
			req.Header.Set("Content-Type", contentType)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := serve(req)
			code := decodeJSONResultCode(t, rec.Body.Bytes())
			if test.wantRejected {
				if code == 0 {
					t.Fatalf("invalid import path accepted: %s", rec.Body.String())
				}
			} else {
				if code != 0 {
					t.Fatalf("import code = %d, body = %s", code, rec.Body.String())
				}
				result := decodeJSONResultData[struct {
					FailedCount int `json:"failedCount"`
					Items       []struct {
						Status string `json:"status"`
					} `json:"items"`
				}](t, rec.Body.Bytes())
				skipped := 0
				for _, item := range result.Items {
					if item.Status == "skipped" {
						skipped++
					}
				}
				if result.FailedCount != 0 || skipped != test.wantSkipped {
					t.Fatalf("expected %d skipped items and no failures: %s", test.wantSkipped, rec.Body.String())
				}
			}
			// Inspect storage directly so hiding a persisted node would not satisfy this test.
			var titles []string
			if err := database.ORM.Model(&models.Node{}).Where(models.NodeColumns.SpaceID+" = ?", spaceID).
				Pluck(models.NodeColumns.Title, &titles).Error; err != nil {
				t.Fatal(err)
			}
			slices.Sort(titles)
			if !slices.Equal(titles, test.wantTitles) {
				t.Fatalf("stored titles = %v, want %v", titles, test.wantTitles)
			}
		})
	}
}

func buildHiddenImportFolderMultipart(t *testing.T, manifest string, paths ...string) (*bytes.Buffer, string) {
	t.Helper()
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	if err := form.WriteField("folderManifest", manifest); err != nil {
		t.Fatal(err)
	}
	for _, filePath := range paths {
		part, err := form.CreateFormFile("files", path.Base(filePath))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(part, "# test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	return &body, form.FormDataContentType()
}
