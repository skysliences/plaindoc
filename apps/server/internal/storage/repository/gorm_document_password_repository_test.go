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

func TestDocumentPasswordsExcludeLockedContentFromDiscovery(t *testing.T) {
	db := openDocumentFormatFilterRepositoryTestDB(t, "file:test-document-passwords?mode=memory&cache=shared")
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	const ownerID = "password-owner"
	const spaceID = "password-space"
	const protectedID = "password-protected"
	const openID = "password-open"
	seedDocumentFormatFilterUser(t, db, ownerID, now)
	seedDocumentFormatFilterSpace(t, db, spaceID, ownerID, models.VisibilityPublic, now)
	for _, documentID := range []string{protectedID, openID} {
		seedDocumentFormatFilterDocument(t, db, documentFormatFilterSeedDocument{
			DocumentID: documentID, NodeID: documentID + "-node", SpaceID: spaceID,
			Title: documentID, Format: models.DocumentFormatMarkdown,
			ContentMD: "document password fixture", Visibility: models.VisibilityPublic, Now: now,
		})
	}

	jobRepo := NewGormSearchIndexJobRepository(db.ORM)
	passwordRepo := NewGormDocumentPasswordRepository(db.ORM, jobRepo)
	password := models.DocumentPassword{
		DocumentID: protectedID, PasswordHash: "first-hash", Generation: "01k00000000000000000000001",
	}
	if err := passwordRepo.Set(ctx, &password); err != nil {
		t.Fatal(err)
	}
	password.PasswordHash = "replacement-hash"
	password.Generation = "01k00000000000000000000002"
	if err := passwordRepo.Set(ctx, &password); err != nil {
		t.Fatal(err)
	}
	loaded, err := passwordRepo.Get(ctx, protectedID)
	if err != nil || *loaded != password {
		t.Fatalf("password rotation did not replace the hash and generation: %+v, %v", loaded, err)
	}
	locked, err := passwordRepo.LockedDocumentIDsBySpace(ctx, spaceID)
	if err != nil || len(locked) != 1 || !locked[protectedID] {
		t.Fatalf("space export lock lookup returned %+v, %v", locked, err)
	}

	visibility := NewGormSearchVisibilityRepository(db.ORM)
	indexSource := NewGormSearchIndexSourceRepository(db.ORM)
	homeSearch := NewGormHomeSearchRepository(db.ORM)
	sitemap := NewGormSitemapRepository(db.ORM)
	candidates := []string{protectedID, openID}
	rows, total, err := visibility.SearchVisibleDocuments(ctx, SearchVisibleDocumentsParams{
		ActorUserID: ownerID, CanViewAllDocuments: true, Terms: []string{"password fixture"}, Limit: 10,
	})
	if err != nil || total != 1 || len(rows) != 1 || rows[0].DocumentID != openID {
		t.Fatalf("database search exposed locked content: %+v, total %d, err %v", rows, total, err)
	}
	ids, err := visibility.FilterVisibleDocumentIDsByCandidates(ctx, SearchVisibleDocumentIDsByCandidatesParams{
		ActorUserID: ownerID, CanViewAllDocuments: true, CandidateDocumentIDs: candidates, IncludeRenderedOffice: true,
	})
	if err != nil || !slices.Equal(ids, []string{openID}) {
		t.Fatalf("stale index candidates exposed locked content: %+v, %v", ids, err)
	}
	if rows, err := homeSearch.ListActiveDocumentMetadataByDocumentIDs(ctx, candidates); err != nil || len(rows) != 1 || rows[0].DocumentID != openID {
		t.Fatalf("homepage search exposed locked content: %+v, %v", rows, err)
	}
	if rows, err := sitemap.ListPublicDocuments(ctx); err != nil || len(rows) != 1 || rows[0].DocumentID != openID {
		t.Fatalf("sitemap exposed locked content: %+v, %v", rows, err)
	}
	if _, err := indexSource.GetActiveDocumentByDocumentID(ctx, protectedID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("index source exposed locked content: %v", err)
	}

	jobs, err := jobRepo.ClaimRunnableJobs(ctx, ClaimSearchIndexJobsParams{Now: now.Add(time.Minute)})
	if err != nil || len(jobs) != 1 || jobs[0].JobType != models.SearchIndexJobTypeDocUpsert ||
		jobs[0].DedupeKey != "doc:"+protectedID {
		t.Fatalf("setting and rotating the lock did not queue one index sync: %+v, %v", jobs, err)
	}
	if err := jobRepo.MarkSuccess(ctx, jobs[0].JobID, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := passwordRepo.Delete(ctx, protectedID); err != nil {
		t.Fatal(err)
	}
	if err := passwordRepo.Delete(ctx, protectedID); err != nil {
		t.Fatal(err)
	}
	if _, err := passwordRepo.Get(ctx, protectedID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("removed password is still present: %v", err)
	}
	if _, err := indexSource.GetActiveDocumentByDocumentID(ctx, protectedID); err != nil {
		t.Fatalf("unlocked content cannot be reindexed: %v", err)
	}
	jobs, err = jobRepo.ClaimRunnableJobs(ctx, ClaimSearchIndexJobsParams{Now: now.Add(time.Minute)})
	if err != nil || len(jobs) != 1 || jobs[0].JobType != models.SearchIndexJobTypeDocUpsert ||
		jobs[0].DedupeKey != "doc:"+protectedID {
		t.Fatalf("removing the lock did not queue one index sync: %+v, %v", jobs, err)
	}
}

func TestDocumentPasswordsResolveManagedLocalObjects(t *testing.T) {
	db := openDocumentFormatFilterRepositoryTestDB(t, "file:test-document-password-managed-objects?mode=memory&cache=shared")
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	now := time.Now().UTC()
	const ownerID = "managed-objects-owner"
	const spaceID = "managed-objects-space"
	const documentID = "managed-objects-document"
	const blobID = "protected-local-blob"
	const imageKey = "images/protected.png"
	const fileKey = "attachments/protected.pdf"
	seedDocumentFormatFilterUser(t, db, ownerID, now)
	seedDocumentFormatFilterSpace(t, db, spaceID, ownerID, models.VisibilityPublic, now)
	seedDocumentFormatFilterDocument(t, db, documentFormatFilterSeedDocument{
		DocumentID: documentID, NodeID: documentID + "-node", SpaceID: spaceID, Title: "Locked assets",
		ContentMD: "Document assets", Format: models.DocumentFormatMarkdown, Visibility: models.VisibilityPublic, Now: now,
	})
	const unlockedDocumentID = "managed-objects-open"
	seedDocumentFormatFilterDocument(t, db, documentFormatFilterSeedDocument{
		DocumentID: unlockedDocumentID, NodeID: unlockedDocumentID + "-node", SpaceID: spaceID, Title: "Unlocked assets",
		ContentMD: "Shared image", Format: models.DocumentFormatMarkdown, Visibility: models.VisibilityPublic, Now: now,
	})
	for _, model := range []any{
		&models.DocumentImageAsset{
			ImageAssetID: "protected-image-asset", DocumentID: documentID, SpaceID: spaceID,
			StorageProvider: "local", ObjectKey: imageKey, Status: "active", LastReferencedAt: now,
		},
		&models.DocumentImageAsset{
			ImageAssetID: "unlocked-image-asset", DocumentID: unlockedDocumentID, SpaceID: spaceID,
			StorageProvider: "local", ObjectKey: imageKey, Status: "active", LastReferencedAt: now,
		},
		&models.DocumentAttachmentBlob{
			BlobID: blobID, StorageProvider: "local", ObjectKey: fileKey,
			MimeType: "application/pdf", ContentHashAlgo: "sha256", ContentHash: "fixture-hash",
		},
		&models.DocumentAttachment{
			AttachmentID: "protected-attachment", BlobID: blobID, DocumentID: documentID, SpaceID: spaceID,
			StorageProvider: "local", ObjectKey: fileKey, FileName: "protected.pdf", Status: models.EntityStatusActive,
			MimeType: "application/pdf", ContentHashAlgo: "sha256", ContentHash: "fixture-hash", PreviewKind: "none",
		},
	} {
		if err := db.ORM.Create(model).Error; err != nil {
			t.Fatal(err)
		}
	}
	passwordRepo := NewGormDocumentPasswordRepository(db.ORM)
	if err := passwordRepo.Set(ctx, &models.DocumentPassword{
		DocumentID: documentID, PasswordHash: "secret-hash", Generation: "01k00000000000000000000001",
	}); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{imageKey, fileKey} {
		ids, err := passwordRepo.LockedDocumentIDsByLocalObjectKey(ctx, key)
		if err != nil || !slices.Equal(ids, []string{documentID}) {
			t.Fatalf("managed object %q lookup returned %+v, %v", key, ids, err)
		}
	}
	if ids, err := passwordRepo.LockedDocumentIDsByLocalObjectKey(ctx, "unrelated-image.png"); err != nil || len(ids) > 0 {
		t.Fatalf("unrelated object lookup returned %+v, %v", ids, err)
	}
}
