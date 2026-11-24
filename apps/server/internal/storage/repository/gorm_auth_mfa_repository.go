package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthMFAChallenge struct {
	SubjectKey     string `gorm:"column:subject_key;primaryKey"`
	TokenHash      string `gorm:"column:token_hash"`
	EncryptedState string `gorm:"column:encrypted_state"`
	AttemptsLeft   int    `gorm:"column:attempts_left"`
	ExpiresAt      int64  `gorm:"column:expires_at"`
}

func (AuthMFAChallenge) TableName() string { return "auth_mfa_challenges" }

type AuthMFARepository struct {
	db *gorm.DB
}

func NewAuthMFARepository(db *gorm.DB) *AuthMFARepository {
	return &AuthMFARepository{db: db}
}

// AuthLoginMFAChallengeSubjectKey isolates attempts for each enrollment generation.
// Keep the original key for accounts that have never been reset.
func AuthLoginMFAChallengeSubjectKey(userID string, generation int64) string {
	subject := "user:" + userID
	if generation != 0 {
		subject += ":" + strconv.FormatInt(generation, 10)
	}
	sum := sha256.Sum256([]byte(subject))
	return hex.EncodeToString(sum[:])
}

func (r *AuthMFARepository) SaveChallenge(ctx context.Context, challenge AuthMFAChallenge, now int64) error {
	table := challenge.TableName()
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "subject_key"}},
		DoUpdates: clause.Assignments(map[string]any{
			"token_hash":      challenge.TokenHash,
			"encrypted_state": challenge.EncryptedState,
			"attempts_left": gorm.Expr(
				"CASE WHEN "+table+".expires_at <= ? THEN ? ELSE "+table+".attempts_left END",
				now, challenge.AttemptsLeft,
			),
			"expires_at": gorm.Expr(
				"CASE WHEN "+table+".expires_at <= ? THEN ? ELSE "+table+".expires_at END",
				now, challenge.ExpiresAt,
			),
		}),
	}).Create(&challenge).Error
}

func (r *AuthMFARepository) DeleteExpired(ctx context.Context, now int64) error {
	return r.db.WithContext(ctx).Where("expires_at <= ?", now).Delete(&AuthMFAChallenge{}).Error
}

func (r *AuthMFARepository) deleteChallengeBySubject(ctx context.Context, subjectKey string) error {
	return r.db.WithContext(ctx).Where("subject_key = ?", subjectKey).Delete(&AuthMFAChallenge{}).Error
}

func (r *AuthMFARepository) GetChallenge(ctx context.Context, tokenHash string, now int64) (AuthMFAChallenge, error) {
	var challenge AuthMFAChallenge
	err := r.activeChallenge(ctx, tokenHash, now).Take(&challenge).Error
	return challenge, err
}

func (r *AuthMFARepository) RecordFailure(ctx context.Context, tokenHash string, now int64) (bool, error) {
	result := r.activeChallenge(ctx, tokenHash, now).UpdateColumn("attempts_left", gorm.Expr("attempts_left - 1"))
	return result.RowsAffected == 1, result.Error
}

func (r *AuthMFARepository) ConsumeChallenge(ctx context.Context, tokenHash string, now int64) (bool, error) {
	result := r.activeChallenge(ctx, tokenHash, now).Delete(&AuthMFAChallenge{})
	return result.RowsAffected == 1, result.Error
}

func (r *AuthMFARepository) activeChallenge(ctx context.Context, tokenHash string, now int64) *gorm.DB {
	return r.db.WithContext(ctx).Model(&AuthMFAChallenge{}).
		Where("token_hash = ?", tokenHash).
		Where("expires_at > ?", now).
		Where("attempts_left > ?", 0)
}

// AcceptTOTPStep records a successful code once, even across concurrent servers.
func (r *AuthMFARepository) AcceptTOTPStep(ctx context.Context, user *models.User, nextSecret string, step int64) (bool, error) {
	columns := models.UserColumns
	result := r.db.WithContext(ctx).Model(&models.User{}).
		Where(columns.UserID+" = ?", user.UserID).
		Where(columns.Status+" = ?", models.EntityStatusActive).
		Where(columns.BannedAt+" IS NULL").
		Where(columns.DeletedAt+" IS NULL").
		Where(columns.PasswordHash+" = ?", user.PasswordHash).
		Where(columns.LoginIPAddressesJSON+" = ?", user.LoginIPAddressesJSON).
		Where(columns.TOTPSecret+" = ?", user.TOTPSecret).
		Where(columns.MFAGeneration+" = ?", user.MFAGeneration).
		Where(columns.TOTPLastStep+" < ?", step).
		UpdateColumns(map[string]any{
			columns.TOTPSecret:   nextSecret,
			columns.TOTPLastStep: step,
		})
	return result.RowsAffected == 1, result.Error
}

func (r *AuthMFARepository) RecordExternalLogin(ctx context.Context, userID, providerID, externalID string, now time.Time) error {
	columns := models.UserIdentityColumns
	return r.db.WithContext(ctx).Model(&models.UserIdentity{}).
		Where(columns.UserID+" = ?", userID).
		Where(columns.ProviderID+" = ?", providerID).
		Where(columns.ExternalID+" = ?", externalID).
		Updates(map[string]any{columns.LastLoginAt: now, columns.UpdatedAt: now}).Error
}
