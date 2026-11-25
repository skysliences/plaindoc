package repository

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

func TestAuthPasskeyRepositoryCredentialRevocation(t *testing.T) {
	ctx := context.Background()
	database := openPasskeyTestDatabase(t)
	repo := NewAuthPasskeyRepository(database.ORM)
	user := models.User{
		UserID:       "01m86aaaaaaaaaaaaaaaaaaaaa",
		Email:        "passkey@example.com",
		PasswordHash: "hashed",
		Name:         "Passkey Test",
		Status:       models.EntityStatusActive,
	}
	if err := database.ORM.WithContext(ctx).Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	credential := models.AuthPasskey{
		PasskeyID:           "01m86bbbbbbbbbbbbbbbbbbbbb",
		CredentialIDHash:    strings.Repeat("a", 64),
		UserID:              user.UserID,
		Name:                "手机",
		EncryptedCredential: "sealed-credential",
		MFAGeneration:       2,
		CreatedAt:           now,
	}
	if err := repo.CreateCredential(ctx, &credential); err != nil {
		t.Fatal(err)
	}
	replacement := credential
	replacement.PasskeyID = "01m86ccccccccccccccccccccc"
	if err := repo.CreateCredential(ctx, &replacement); err == nil {
		t.Fatal("duplicate credential ID accepted")
	}
	credentials, err := repo.ListCredentials(ctx, user.UserID)
	if err != nil || len(credentials) != 1 ||
		credentials[0].MFAGeneration != 2 || !credentials[0].CreatedAt.Equal(now) {
		t.Fatalf("unexpected credentials: %+v, %v", credentials, err)
	}
	if deleted, err := repo.DeleteCredential(ctx, "another-user", credential.PasskeyID); err != nil || deleted {
		t.Fatalf("another user could delete this credential: %v, %v", deleted, err)
	}
	usedAt := now.Add(time.Second)
	if updated, err := repo.UpdateCredential(ctx, credential.PasskeyID, 0, "updated-sealed-credential", usedAt); err != nil || !updated {
		t.Fatalf("update credential: %v, %v", updated, err)
	}
	if updated, err := repo.UpdateCredential(ctx, credential.PasskeyID, 0, credential.EncryptedCredential, usedAt); err != nil || updated {
		t.Fatalf("stale write affected credential: %v, %v", updated, err)
	}
	loaded, err := repo.GetCredential(ctx, credential.CredentialIDHash)
	if err != nil || loaded.LastUsedAt == nil || !loaded.LastUsedAt.Equal(usedAt) ||
		loaded.EncryptedCredential != "updated-sealed-credential" || loaded.Version != 1 {
		t.Fatalf("unexpected updated credential: %+v, %v", loaded, err)
	}
	if deleted, err := repo.DeleteCredential(ctx, user.UserID, credential.PasskeyID); err != nil || !deleted {
		t.Fatalf("delete credential: %v, %v", deleted, err)
	}
	if _, err := repo.GetCredential(ctx, credential.CredentialIDHash); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("revoked credential is still visible: %v", err)
	}
	if err := repo.CreateCredential(ctx, &replacement); err != nil {
		t.Fatal(err)
	}
	if updated, err := repo.UpdateCredential(ctx, credential.PasskeyID, 1, credential.EncryptedCredential, usedAt); err != nil || updated {
		t.Fatalf("revoked enrollment affected replacement: %v, %v", updated, err)
	}
}

func TestAuthPasskeyRepositoryChallengeBindingAndConsumption(t *testing.T) {
	ctx := context.Background()
	database := openPasskeyTestDatabase(t)
	repo := NewAuthPasskeyRepository(database.ORM)
	const now = int64(1000)
	challenge := models.AuthPasskeyChallenge{
		TokenHash:   strings.Repeat("a", 64),
		UserID:      "01m86aaaaaaaaaaaaaaaaaaaaa",
		Purpose:     "register",
		SessionJSON: `{"session":"challenge-state"}`,
		ExpiresAt:   now + 60,
	}
	if err := repo.SaveChallenge(ctx, &challenge, now); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetChallenge(ctx, challenge.TokenHash, "login", challenge.UserID, now); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("challenge can be read with a different purpose: %v", err)
	}
	if consumed, err := repo.ConsumeChallenge(ctx, challenge.TokenHash, challenge.Purpose, "", now); err != nil || consumed {
		t.Fatalf("challenge can be consumed by a different user: %v, %v", consumed, err)
	}
	if _, err := repo.GetChallenge(ctx, challenge.TokenHash, challenge.Purpose, challenge.UserID, challenge.ExpiresAt); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expired challenge is readable: %v", err)
	}
	loaded, err := repo.GetChallenge(ctx, challenge.TokenHash, challenge.Purpose, challenge.UserID, now)
	if err != nil || loaded.SessionJSON != challenge.SessionJSON {
		t.Fatalf("unexpected challenge state: %+v, %v", loaded, err)
	}
	if consumed, err := repo.ConsumeChallenge(ctx, challenge.TokenHash, challenge.Purpose, challenge.UserID, now); err != nil || !consumed {
		t.Fatalf("consume challenge: %v, %v", consumed, err)
	}
	if consumed, err := repo.ConsumeChallenge(ctx, challenge.TokenHash, challenge.Purpose, challenge.UserID, now); err != nil || consumed {
		t.Fatalf("challenge replay succeeded: %v, %v", consumed, err)
	}

	expired := challenge
	expired.TokenHash = strings.Repeat("b", 64)
	if err := repo.SaveChallenge(ctx, &expired, now); err != nil {
		t.Fatal(err)
	}
	replacement := expired
	replacement.ExpiresAt += 60
	if err := repo.SaveChallenge(ctx, &replacement, expired.ExpiresAt); err != nil {
		t.Fatalf("expired row was not cleaned before saving: %v", err)
	}
}

func openPasskeyTestDatabase(t *testing.T) *storage.Database {
	t.Helper()
	database, err := storage.OpenDatabase(storage.OpenConfig{
		Driver: storage.DriverSQLite,
		DSN:    "file:" + t.Name() + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storage.MigrateUp(context.Background(), database.ORM, storage.DriverSQLite); err != nil {
		t.Fatal(err)
	}
	return database
}
