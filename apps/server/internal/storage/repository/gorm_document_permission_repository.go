package repository

import (
	"context"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type DocumentPermissionRepository interface {
	List(ctx context.Context, spaceID, documentID, userID string) ([]models.DocumentPermission, error)
	Set(ctx context.Context, permission models.DocumentPermission, updateAccess bool, exportDenied *bool) error
}

type gormDocumentPermissionRepository struct {
	db *gorm.DB
}

func NewGormDocumentPermissionRepository(db *gorm.DB) DocumentPermissionRepository {
	return &gormDocumentPermissionRepository{db: db}
}

func (r *gormDocumentPermissionRepository) List(
	ctx context.Context, spaceID, documentID, userID string,
) ([]models.DocumentPermission, error) {
	query := r.db.WithContext(ctx).
		Table(tableWithAlias(models.DocumentPermission{}, "dp")).
		Select("dp.*").
		Joins("JOIN "+tableName(models.Document{})+" AS d ON d.document_id = dp.document_id").
		Joins("JOIN "+tableName(models.Node{})+" AS n ON n.node_id = d.node_id").
		Joins("JOIN "+tableName(models.Space{})+" AS s ON s.space_id = n.space_id").
		Where("s.owner_user_id <> dp.user_id").
		Where("d.status = ?", models.EntityStatusActive).
		Where("d.deleted_at IS NULL")
	if spaceID != "" {
		query = query.Where("s.space_id = ?", spaceID)
	}
	if documentID != "" {
		query = query.Where("dp.document_id = ?", documentID)
	}
	if userID != "" {
		query = query.Where("dp.user_id = ?", userID)
	}

	var permissions []models.DocumentPermission
	err := query.Find(&permissions).Error
	return permissions, err
}

func (r *gormDocumentPermissionRepository) Set(
	ctx context.Context, permission models.DocumentPermission, updateAccess bool, exportDenied *bool,
) error {
	columns := models.DocumentPermissionColumns
	updates := []string{columns.GrantedByUserID, columns.UpdatedAt}
	if updateAccess {
		updates = append(updates, columns.Role, columns.InheritAccess, columns.AccessDenied, columns.CopyDenied)
	}
	if exportDenied != nil {
		permission.ExportDenied = *exportDenied
		updates = append(updates, columns.ExportDenied)
	}

	// The upsert changes only the supplied settings so concurrent updates to
	// access and export cannot overwrite each other.
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: columns.DocumentID}, {Name: columns.UserID}},
			DoUpdates: clause.AssignmentColumns(updates),
		}).Create(&permission).Error; err != nil {
			return err
		}
		return tx.Where(&models.DocumentPermission{DocumentID: permission.DocumentID, UserID: permission.UserID}).
			Where(columns.InheritAccess+" = ?", true).
			Where(columns.ExportDenied+" = ?", false).
			Delete(&models.DocumentPermission{}).Error
	})
}
