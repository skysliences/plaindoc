package repository

import (
	"context"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

type gormAuthPasskeyRepository struct {
	db *gorm.DB
}

func NewAuthPasskeyRepository(db *gorm.DB) AuthPasskeyRepository {
	return &gormAuthPasskeyRepository{db: db}
}

func (r *gormAuthPasskeyRepository) ListCredentials(ctx context.Context, userID string) ([]models.AuthPasskey, error) {
	columns := models.AuthPasskeyColumns
	credentials := make([]models.AuthPasskey, 0)
	err := r.db.WithContext(ctx).
		Where(columns.UserID+" = ?", userID).
		Order(columns.CreatedAt + " DESC").
		Order(columns.PasskeyID + " DESC").
		Find(&credentials).Error
	return credentials, err
}

func (r *gormAuthPasskeyRepository) GetCredential(ctx context.Context, credentialIDHash string) (*models.AuthPasskey, error) {
	var credential models.AuthPasskey
	if err := r.db.WithContext(ctx).
		Where(models.AuthPasskeyColumns.CredentialIDHash+" = ?", credentialIDHash).
		Take(&credential).Error; err != nil {
		return nil, err
	}
	return &credential, nil
}

func (r *gormAuthPasskeyRepository) CreateCredential(ctx context.Context, credential *models.AuthPasskey) error {
	return r.db.WithContext(ctx).Create(credential).Error
}

// UpdateCredential rejects concurrent updates and updates to a deleted enrollment.
func (r *gormAuthPasskeyRepository) UpdateCredential(
	ctx context.Context,
	passkeyID string,
	previousVersion int64,
	encryptedCredential string,
	usedAt time.Time,
) (bool, error) {
	columns := models.AuthPasskeyColumns
	result := r.db.WithContext(ctx).Model(&models.AuthPasskey{}).
		Where(columns.PasskeyID+" = ?", passkeyID).
		Where(columns.Version+" = ?", previousVersion).
		UpdateColumns(map[string]any{
			columns.EncryptedCredential: encryptedCredential,
			columns.LastUsedAt:          usedAt.UTC(),
			columns.Version:             gorm.Expr(columns.Version + " + 1"),
		})
	return result.RowsAffected == 1, result.Error
}

func (r *gormAuthPasskeyRepository) DeleteCredential(ctx context.Context, userID, passkeyID string) (bool, error) {
	columns := models.AuthPasskeyColumns
	result := r.db.WithContext(ctx).
		Where(columns.PasskeyID+" = ?", passkeyID).
		Where(columns.UserID+" = ?", userID).
		Delete(&models.AuthPasskey{})
	return result.RowsAffected == 1, result.Error
}

func (r *gormAuthPasskeyRepository) SaveChallenge(ctx context.Context, challenge *models.AuthPasskeyChallenge, now int64) error {
	db := r.db.WithContext(ctx)
	if err := db.Where(models.AuthPasskeyChallengeColumns.ExpiresAt+" <= ?", now).
		Delete(&models.AuthPasskeyChallenge{}).Error; err != nil {
		return err
	}
	return db.Create(challenge).Error
}

func (r *gormAuthPasskeyRepository) GetChallenge(
	ctx context.Context,
	tokenHash, purpose, userID string,
	now int64,
) (*models.AuthPasskeyChallenge, error) {
	var challenge models.AuthPasskeyChallenge
	if err := r.activeChallenge(ctx, tokenHash, purpose, userID, now).Take(&challenge).Error; err != nil {
		return nil, err
	}
	return &challenge, nil
}

// ConsumeChallenge atomically grants at most one submission for an unexpired challenge.
func (r *gormAuthPasskeyRepository) ConsumeChallenge(
	ctx context.Context,
	tokenHash, purpose, userID string,
	now int64,
) (bool, error) {
	result := r.activeChallenge(ctx, tokenHash, purpose, userID, now).Delete(&models.AuthPasskeyChallenge{})
	return result.RowsAffected == 1, result.Error
}

func (r *gormAuthPasskeyRepository) activeChallenge(ctx context.Context, tokenHash, purpose, userID string, now int64) *gorm.DB {
	columns := models.AuthPasskeyChallengeColumns
	return r.db.WithContext(ctx).
		Where(columns.TokenHash+" = ?", tokenHash).
		Where(columns.Purpose+" = ?", purpose).
		Where(columns.UserID+" = ?", userID).
		Where(columns.ExpiresAt+" > ?", now)
}
