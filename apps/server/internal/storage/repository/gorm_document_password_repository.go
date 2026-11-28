package repository

import (
	"context"
	"strings"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type gormDocumentPasswordRepository struct {
	db                 *gorm.DB
	searchIndexJobRepo SearchIndexJobRepository
}

func NewGormDocumentPasswordRepository(
	db *gorm.DB,
	searchIndexJobRepos ...SearchIndexJobRepository,
) DocumentPasswordRepository {
	repo := &gormDocumentPasswordRepository{db: db}
	if len(searchIndexJobRepos) > 0 {
		repo.searchIndexJobRepo = searchIndexJobRepos[0]
	}
	return repo
}

func (r *gormDocumentPasswordRepository) Get(ctx context.Context, documentID string) (*models.DocumentPassword, error) {
	var password models.DocumentPassword
	if err := r.db.WithContext(ctx).
		Where(models.DocumentPasswordColumns.DocumentID+" = ?", documentID).
		Take(&password).Error; err != nil {
		return nil, err
	}
	return &password, nil
}

func (r *gormDocumentPasswordRepository) Set(ctx context.Context, password *models.DocumentPassword) error {
	columns := models.DocumentPasswordColumns
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: columns.DocumentID}},
			DoUpdates: clause.AssignmentColumns([]string{columns.PasswordHash, columns.Generation}),
		}).Create(password).Error; err != nil {
			return err
		}
		return r.syncSearchInTx(ctx, tx, password.DocumentID)
	})
}

func (r *gormDocumentPasswordRepository) Delete(ctx context.Context, documentID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Where(models.DocumentPasswordColumns.DocumentID+" = ?", documentID).
			Delete(&models.DocumentPassword{})
		if result.Error != nil || result.RowsAffected == 0 {
			return result.Error
		}
		return r.syncSearchInTx(ctx, tx, documentID)
	})
}

func (r *gormDocumentPasswordRepository) LockedDocumentIDsBySpace(ctx context.Context, spaceID string) (map[string]bool, error) {
	passwordDocumentID := qualifiedColumn("dp", models.DocumentPasswordColumns.DocumentID)
	var documentIDs []string
	if err := r.db.WithContext(ctx).
		Table(tableWithAlias(models.DocumentPassword{}, "dp")).
		Joins("JOIN "+tableWithAlias(models.Document{}, "d")+" ON "+qualifiedColumn("d", models.DocumentColumns.DocumentID)+" = "+passwordDocumentID).
		Joins("JOIN "+tableWithAlias(models.Node{}, "n")+" ON "+qualifiedColumn("n", models.NodeColumns.NodeID)+" = "+qualifiedColumn("d", models.DocumentColumns.NodeID)).
		Where(qualifiedColumn("n", models.NodeColumns.SpaceID)+" = ?", spaceID).
		Pluck(passwordDocumentID, &documentIDs).Error; err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(documentIDs))
	for _, documentID := range documentIDs {
		result[documentID] = true
	}
	return result, nil
}

// LockedDocumentIDsByLocalObjectKey resolves all live document locks for managed local content.
func (r *gormDocumentPasswordRepository) LockedDocumentIDsByLocalObjectKey(ctx context.Context, objectKey string) ([]string, error) {
	objectKey = strings.TrimSpace(objectKey)
	if objectKey == "" {
		return nil, nil
	}
	db := r.db.WithContext(ctx)
	objectKeys := []string{objectKey, "local/" + objectKey} // The local file handler also resolves the historical prefix.
	image := models.DocumentImageAssetColumns
	imageDocuments := db.Model(&models.DocumentImageAsset{}).
		Select(image.DocumentID).
		Where(image.StorageProvider+" = ?", "local").
		Where(image.ObjectKey+" IN ?", objectKeys).
		Where(image.DeletedAt+" IS NULL").
		Where(image.Status+" <> ?", "deleted")
	localBlobs := db.Model(&models.DocumentAttachmentBlob{}).
		Select(models.DocumentAttachmentBlobColumns.BlobID).
		Where(models.DocumentAttachmentBlobColumns.StorageProvider+" = ?", "local").
		Where(models.DocumentAttachmentBlobColumns.ObjectKey+" IN ?", objectKeys).
		Where(models.DocumentAttachmentBlobColumns.DeletedAt + " IS NULL")
	attachment := models.DocumentAttachmentColumns
	activeAttachments := db.Model(&models.DocumentAttachment{}).
		Select(attachment.DocumentID).
		Where(attachment.Status+" = ?", models.EntityStatusActive).
		Where(attachment.DeletedAt + " IS NULL")
	attachmentDocuments := activeAttachments.Session(&gorm.Session{}).
		Where(attachment.StorageProvider+" = ?", "local").
		Where(attachment.ObjectKey+" IN ?", objectKeys)
	blobAttachmentDocuments := activeAttachments.Session(&gorm.Session{}).
		Where(attachment.BlobID+" IN (?)", localBlobs)
	sourceDocuments := db.Model(&models.Document{}).
		Select(models.DocumentColumns.DocumentID).
		Where(models.DocumentColumns.SourceBlobID+" IN (?)", localBlobs)
	fileRevisionDocuments := db.Model(&models.DocumentFileRevision{}).
		Select(models.DocumentFileRevisionColumns.DocumentID).
		Where(models.DocumentFileRevisionColumns.BlobID+" IN (?)", localBlobs)
	references := db.Raw("? UNION ? UNION ? UNION ? UNION ?", imageDocuments, attachmentDocuments,
		blobAttachmentDocuments, sourceDocuments, fileRevisionDocuments)

	documentID := models.DocumentPasswordColumns.DocumentID
	var locked []string
	err := db.Model(&models.DocumentPassword{}).
		Where(documentID+" IN (?)", references).
		Pluck(documentID, &locked).Error
	return locked, err
}

func (r *gormDocumentPasswordRepository) syncSearchInTx(ctx context.Context, tx *gorm.DB, documentID string) error {
	if r.searchIndexJobRepo == nil {
		return nil
	}
	// Resolve the current lock state when the job runs, including when changes coalesce.
	params, err := BuildSearchIndexDocUpsertJob(documentID)
	if err != nil {
		return err
	}
	return r.searchIndexJobRepo.EnqueueInTx(ctx, tx, params)
}

// excludePasswordProtectedDocuments keeps locked content out of public and internal discovery paths.
func excludePasswordProtectedDocuments(query *gorm.DB, documentAlias string) *gorm.DB {
	lock := query.Session(&gorm.Session{NewDB: true}).
		Model(&models.DocumentPassword{}).
		Select("1").
		Where(models.DocumentPasswordColumns.DocumentID + " = " + qualifiedColumn(documentAlias, models.DocumentColumns.DocumentID))
	return query.Where("NOT EXISTS (?)", lock)
}
