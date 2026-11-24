package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/lifei6671/plaindoc/apps/server/internal/config"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

func newAuthMFATestService(t *testing.T) (*AuthService, *storage.Database) {
	t.Helper()
	database, err := storage.OpenDatabase(storage.OpenConfig{
		Driver: storage.DriverSQLite,
		DSN:    "file:auth-mfa-" + ulid.Make().String() + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storage.MigrateUp(context.Background(), database.ORM, storage.DriverSQLite); err != nil {
		t.Fatal(err)
	}
	return NewAuthService(
		repository.NewGormUserRepository(database.ORM),
		repository.NewGormUserSessionRepository(database.ORM),
		config.JWTConfig{
			Secret:          "test-key-for-mfa-implementation",
			AccessTokenTTL:  time.Hour,
			RefreshTokenTTL: 24 * time.Hour,
		},
		repository.NewAuthMFARepository(database.ORM),
	), database
}

func TestAuthMFARegistrationAndLoginRequireProof(t *testing.T) {
	ctx := context.Background()
	auth, database := newAuthMFATestService(t)
	now := time.Unix(1_700_000_010, 0)
	auth.mfaNow = func() time.Time { return now }

	challenge, err := auth.BeginRegistration(ctx, " new@example.com ", "registration-password", "Test User")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.Mode != authMFASetupMode || !strings.HasPrefix(challenge.OTPAuthURL, "otpauth://totp/") ||
		!strings.HasPrefix(challenge.QRCodeDataURL, "data:image/png;base64,") || challenge.Secret == "" {
		t.Fatal("registration did not return valid setup instructions")
	}
	if _, err := auth.userRepo.GetByEmail(ctx, "new@example.com"); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("registration created the account before TOTP proof: %v", err)
	}
	var sessions int64
	if err := database.ORM.Model(&models.UserSession{}).Count(&sessions).Error; err != nil || sessions != 0 {
		t.Fatalf("sessions before TOTP proof: %d, %v", sessions, err)
	}
	var pending repository.AuthMFAChallenge
	if err := database.ORM.Take(&pending).Error; err != nil {
		t.Fatal(err)
	}
	if pending.TokenHash == challenge.ChallengeToken || strings.Contains(pending.EncryptedState, challenge.Secret) {
		t.Fatal("stored challenge exposed a bearer token or setup secret")
	}

	setupCode := testAuthMFACode(t, challenge.Secret, now)
	if _, _, err := auth.VerifyMFA(ctx, challenge.ChallengeToken, setupCode, func(context.Context) (bool, error) {
		return false, nil
	}); !errors.Is(err, ErrAuthRegistrationClosed) {
		t.Fatalf("expected registration policy check on completion: %v", err)
	}
	if _, _, err := auth.VerifyMFA(ctx, challenge.ChallengeToken, "wrong!", nil); !errors.Is(err, ErrAuthMFACodeInvalid) {
		t.Fatalf("expected registration code failure: %v", err)
	}
	session, _, err := auth.VerifyMFA(ctx, challenge.ChallengeToken, setupCode, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Me(ctx, session.Token); err != nil {
		t.Fatalf("verified session unusable: %v", err)
	}
	user, err := auth.userRepo.GetByUserID(ctx, session.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if user.TOTPSecret == "" || strings.Contains(user.TOTPSecret, challenge.Secret) {
		t.Fatal("stored TOTP credential missing or unencrypted")
	}
	if _, _, err := auth.VerifyMFA(ctx, challenge.ChallengeToken, setupCode, nil); !errors.Is(err, ErrAuthMFAChallengeExpired) {
		t.Fatalf("used challenge remains valid: %v", err)
	}

	identity, err := auth.Login(ctx, user.Email, "registration-password")
	if err != nil {
		t.Fatal(err)
	}
	login, err := auth.BeginLoginMFA(ctx, identity, user.Email)
	if err != nil {
		t.Fatal(err)
	}
	if login.Mode != authMFAVerifyMode || login.Secret != "" || login.OTPAuthURL != "" || login.QRCodeDataURL != "" {
		t.Fatal("already enrolled login revealed setup material")
	}
	if _, _, err := auth.VerifyMFA(ctx, login.ChallengeToken, setupCode, nil); !errors.Is(err, ErrAuthMFACodeInvalid) {
		t.Fatalf("a used TOTP was accepted again: %v", err)
	}
	now = now.Add(authTOTPPeriod * time.Second)
	_, _, err = auth.VerifyMFA(ctx, login.ChallengeToken, testAuthMFACode(t, challenge.Secret, now), nil)
	if err != nil {
		t.Fatalf("login did not accept next TOTP time step: %v", err)
	}
}

func TestAuthMFAExistingAccountAndSharedAttemptLimit(t *testing.T) {
	ctx := context.Background()
	auth, _ := newAuthMFATestService(t)
	now := time.Unix(1_700_000_010, 0)
	auth.mfaNow = func() time.Time { return now }
	password, err := bcrypt.GenerateFromPassword([]byte("existing-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	user := &models.User{
		UserID:       ulid.Make().String(),
		Email:        "existing@example.com",
		Name:         "Existing User",
		PasswordHash: string(password),
	}
	if err := auth.userRepo.Create(ctx, user); err != nil {
		t.Fatal(err)
	}
	identity, err := auth.Login(ctx, user.Email, "existing-password")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < authMFAAttempts; i++ {
		challenge, err := auth.BeginLoginMFA(ctx, identity, user.Email)
		if err != nil {
			t.Fatalf("creation attempt %d: %v", i, err)
		}
		if challenge.Mode != authMFASetupMode {
			t.Fatal("unenrolled account was not asked to set up its authenticator")
		}
		if _, _, err := auth.VerifyMFA(ctx, challenge.ChallengeToken, "wrong!", nil); !errors.Is(err, ErrAuthMFACodeInvalid) {
			t.Fatalf("expected wrong code failure: %v", err)
		}
	}
	if _, err := auth.BeginLoginMFA(ctx, identity, user.Email); !errors.Is(err, ErrAuthMFAChallengeExpired) {
		t.Fatalf("restarting login bypassed the shared attempt limit: %v", err)
	}
	now = now.Add(authMFAChallengeTTL)
	setup, err := auth.BeginLoginMFA(ctx, identity, user.Email)
	if err != nil {
		t.Fatalf("could not begin fresh challenge after timeout: %v", err)
	}
	if _, _, err := auth.VerifyMFA(ctx, setup.ChallengeToken, testAuthMFACode(t, setup.Secret, now), nil); err != nil {
		t.Fatalf("existing account could not enroll: %v", err)
	}

	legacy := authTokenClaims{
		TokenType: authTokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   user.UserID,
			ID:        "legacy-session",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, legacy).SignedString(auth.jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := auth.Me(ctx, token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("pre-MFA access token was accepted: %v", err)
	}
}

func TestAuthTOTPCodeMatchesRFC6238SHA1(t *testing.T) {
	// RFC 6238 SHA-1 output for Unix time 59, truncated to six digits.
	if got := authTOTPCode([]byte("12345678901234567890"), 59/authTOTPPeriod); got != "287082" {
		t.Fatalf("unexpected RFC TOTP code: %s", got)
	}
}

func testAuthMFACode(t *testing.T, secret string, now time.Time) string {
	t.Helper()
	key, err := authTOTPEncoding.DecodeString(secret)
	if err != nil {
		t.Fatal("invalid base32 test setup secret")
	}
	return authTOTPCode(key, now.Unix()/authTOTPPeriod)
}
