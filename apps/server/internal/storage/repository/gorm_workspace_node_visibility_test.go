package repository

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

func TestHiddenWorkspacePathsAreExcludedBeforeSearchPagination(t *testing.T) {
	database := openDocumentFormatFilterRepositoryTestDB(t, "file:test-hidden-search-paths?mode=memory&cache=shared")
	t.Cleanup(func() { _ = database.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	seedDocumentFormatFilterUser(t, database, "dot-owner", now)
	seedDocumentFormatFilterSpace(t, database, "dot-space", "dot-owner", models.VisibilityPublic, now)
	folders := []models.Node{
		{NodeID: "git", SpaceID: "dot-space", Type: models.NodeTypeFolder, Title: ".git"},
		{NodeID: "hooks", SpaceID: "dot-space", ParentNodeID: stringPointer("git"), Type: models.NodeTypeFolder, Title: "hooks"},
	}
	if err := database.ORM.Create(&folders).Error; err != nil {
		t.Fatal(err)
	}
	for i, document := range []struct {
		id       string
		title    string
		parentID string
	}{
		{id: "first-visible", title: "notes.v1.md"},
		{id: "second-visible", title: "visible.md"},
		{id: "direct-hidden", title: "._README.md"},
		{id: "nested-hidden", title: "pre-commit.md", parentID: "hooks"},
	} {
		seedDocumentFormatFilterDocument(t, database, documentFormatFilterSeedDocument{
			SpaceID: "dot-space", NodeID: document.id + "-node", DocumentID: document.id, Title: document.title,
			ContentMD: "hidden-path search fixture", Format: models.DocumentFormatMarkdown, Visibility: models.VisibilityPublic,
			Now: now.Add(time.Duration(i) * time.Second),
		})
		if document.parentID != "" {
			if err := database.ORM.Model(&models.Node{}).Where(models.NodeColumns.NodeID+" = ?", document.id+"-node").
				Update(models.NodeColumns.ParentNodeID, document.parentID).Error; err != nil {
				t.Fatal(err)
			}
		}
	}
	repository := NewGormSearchVisibilityRepository(database.ORM)
	for i, expected := range []string{"second-visible", "first-visible"} {
		rows, total, err := repository.SearchVisibleDocuments(ctx, SearchVisibleDocumentsParams{
			Terms: []string{"search fixture"}, Limit: 1, Offset: i,
		})
		if err != nil || total != 2 || len(rows) != 1 || rows[0].DocumentID != expected {
			t.Fatalf("search page %d: got %+v, total %d, err %v", i, rows, total, err)
		}
	}
	candidates := []string{"direct-hidden", "nested-hidden", "second-visible", "first-visible"}
	expected := []string{"first-visible", "second-visible"}
	ids, err := repository.FilterVisibleDocumentIDsByCandidates(ctx, SearchVisibleDocumentIDsByCandidatesParams{
		CandidateDocumentIDs: candidates, ActorUserID: "dot-owner", CanViewAllDocuments: true,
	})
	slices.Sort(ids)
	if err != nil || !slices.Equal(ids, expected) {
		t.Fatalf("Bleve candidates: got %v, err %v", ids, err)
	}
	metadata, err := NewGormHomeSearchRepository(database.ORM).ListActiveDocumentMetadataByDocumentIDs(ctx, candidates)
	if err != nil {
		t.Fatal(err)
	}
	ids = ids[:0]
	for _, row := range metadata {
		ids = append(ids, row.DocumentID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, expected) {
		t.Fatalf("homepage search metadata: got %v", ids)
	}
	source := NewGormSearchIndexSourceRepository(database.ORM)
	if _, err := source.GetActiveDocumentByDocumentID(ctx, "nested-hidden"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("hidden index source: got %v", err)
	}
	rows, err := source.ListActiveDocuments(ctx, ListSearchIndexSourceDocumentsParams{Limit: 10})
	if err != nil || len(rows) != 2 {
		t.Fatalf("index sources: got %+v, err %v", rows, err)
	}
	publicDocs, err := NewGormSitemapRepository(database.ORM).ListPublicDocuments(ctx)
	if err != nil || len(publicDocs) != 2 ||
		publicDocs[0].DocumentID != "first-visible" || publicDocs[1].DocumentID != "second-visible" {
		t.Fatalf("sitemap documents: got %+v, err %v", publicDocs, err)
	}
}
