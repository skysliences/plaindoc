package service

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/config"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	qrcode "github.com/skip2/go-qrcode"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

const (
	authMFAChallengeTTL = 5 * time.Minute
	authMFAAttempts     = 5
	authTOTPPeriod      = 30
	authTOTPSecretSize  = 20
	authMFASetupMode    = "setup"
	authMFAVerifyMode   = "verify"
)

var (
	ErrAuthMFAChallengeExpired = errors.New("mfa challenge expired or invalid")
	ErrAuthMFACodeInvalid      = errors.New("mfa code invalid")
	ErrAuthRegistrationClosed  = errors.New("registration is disabled")
	authTOTPEncoding           = base32.StdEncoding.WithPadding(base32.NoPadding)
)

type AuthMFAChallenge struct {
	ChallengeToken string `json:"challengeToken"`
	Mode           string `json:"mode"`
	Secret         string `json:"secret,omitempty"`
	OTPAuthURL     string `json:"otpauthUrl,omitempty"`
	QRCodeDataURL  string `json:"qrCodeDataUrl,omitempty"`
}

// AuthMFAAttempt identifies the initial request for the existing authentication risk controls.
type AuthMFAAttempt struct {
	Scene      string
	Identifier string
}

type authMFAPending struct {
	Attempt            AuthMFAAttempt
	UserID             string
	Email              string
	Name               string
	PasswordHash       string
	PasswordProof      string
	MFAGeneration      int64
	SetupSecret        string
	ExternalProviderID string
	ExternalID         string
}

// BeginRegistration verifies available account details without creating an account or session.
func (s *AuthService) BeginRegistration(ctx context.Context, email, password, name string) (AuthMFAChallenge, error) {
	if s == nil || s.userRepo == nil || s.mfaRepo == nil {
		return AuthMFAChallenge{}, errors.New("auth service dependencies are nil")
	}
	normalizedEmail := normalizeEmail(email)
	if _, err := s.userRepo.GetByEmail(ctx, normalizedEmail); err == nil {
		return AuthMFAChallenge{}, ErrEmailAlreadyExists
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthMFAChallenge{}, err
	}

	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return AuthMFAChallenge{}, err
	}
	return s.beginMFA(ctx, hashToken("register:"+normalizedEmail), authMFAPending{
		Attempt:      AuthMFAAttempt{Scene: "register", Identifier: normalizedEmail},
		Email:        normalizedEmail,
		Name:         strings.TrimSpace(name),
		PasswordHash: string(passwordHash),
	}, true)
}

// BeginLoginMFA starts the second factor after a provider verifies the first one.
func (s *AuthService) BeginLoginMFA(ctx context.Context, identity AuthPrimaryIdentity, identifier string) (AuthMFAChallenge, error) {
	if s == nil || s.userRepo == nil || s.mfaRepo == nil {
		return AuthMFAChallenge{}, errors.New("auth service dependencies are nil")
	}
	user, err := s.userRepo.GetByUserID(ctx, identity.User.ID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthMFAChallenge{}, ErrInvalidCredentials
	}
	if err != nil {
		return AuthMFAChallenge{}, err
	}
	if err := ensureAuthUserActive(user); err != nil {
		return AuthMFAChallenge{}, err
	}
	if err := ensureAuthUserLoginIPAllowed(ctx, user); err != nil {
		return AuthMFAChallenge{}, err
	}
	if hashToken(user.PasswordHash) != identity.passwordProof || user.MFAGeneration != identity.mfaGeneration {
		return AuthMFAChallenge{}, ErrInvalidCredentials
	}

	return s.beginMFA(ctx, repository.AuthLoginMFAChallengeSubjectKey(user.UserID, user.MFAGeneration), authMFAPending{
		Attempt:            AuthMFAAttempt{Scene: "login", Identifier: identifier},
		UserID:             user.UserID,
		Email:              user.Email,
		PasswordProof:      identity.passwordProof,
		MFAGeneration:      user.MFAGeneration,
		ExternalProviderID: identity.externalProviderID,
		ExternalID:         identity.externalID,
	}, user.TOTPSecret == "")
}

func (s *AuthService) beginMFA(ctx context.Context, subjectKey string, pending authMFAPending, setup bool) (AuthMFAChallenge, error) {
	token := base64.RawURLEncoding.EncodeToString(randomAuthMFABytes(32))
	response := AuthMFAChallenge{ChallengeToken: token, Mode: authMFAVerifyMode}
	if setup {
		pending.SetupSecret = authTOTPEncoding.EncodeToString(randomAuthMFABytes(authTOTPSecretSize))
		response.Mode = authMFASetupMode
		response.Secret = pending.SetupSecret
		response.OTPAuthURL = buildAuthTOTPURL(pending.Email, pending.SetupSecret)
		qrPNG, err := qrcode.Encode(response.OTPAuthURL, qrcode.Medium, 256)
		if err != nil {
			return AuthMFAChallenge{}, err
		}
		response.QRCodeDataURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(qrPNG)
	}

	raw, err := json.Marshal(pending)
	if err != nil {
		return AuthMFAChallenge{}, err
	}
	tokenHash := hashToken(token)
	encryptedState, err := s.sealMFA("challenge:"+tokenHash, raw)
	if err != nil {
		return AuthMFAChallenge{}, err
	}

	now := s.mfaNow()
	if err := s.mfaRepo.DeleteExpired(ctx, now.Unix()); err != nil {
		return AuthMFAChallenge{}, err
	}
	if err := s.mfaRepo.SaveChallenge(ctx, repository.AuthMFAChallenge{
		SubjectKey:     subjectKey,
		TokenHash:      tokenHash,
		EncryptedState: encryptedState,
		AttemptsLeft:   authMFAAttempts,
		ExpiresAt:      now.Add(authMFAChallengeTTL).Unix(),
	}, now.Unix()); err != nil {
		return AuthMFAChallenge{}, err
	}
	if _, err := s.mfaRepo.GetChallenge(ctx, tokenHash, now.Unix()); errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthMFAChallenge{}, ErrAuthMFAChallengeExpired
	} else if err != nil {
		return AuthMFAChallenge{}, err
	}
	return response, nil
}

// VerifyMFA completes registration or login and creates a session only after proof of TOTP possession.
func (s *AuthService) VerifyMFA(
	ctx context.Context,
	token, code string,
	allowRegistration func(context.Context) (bool, error),
) (AuthSession, AuthMFAAttempt, error) {
	var pending authMFAPending
	if s == nil || s.userRepo == nil || s.userSessionRepo == nil || s.mfaRepo == nil {
		return AuthSession{}, pending.Attempt, errors.New("auth service dependencies are nil")
	}
	token = strings.TrimSpace(token)
	if len(token) != base64.RawURLEncoding.EncodedLen(32) {
		return AuthSession{}, pending.Attempt, ErrAuthMFAChallengeExpired
	}
	now := s.mfaNow()
	tokenHash := hashToken(token)
	challenge, err := s.mfaRepo.GetChallenge(ctx, tokenHash, now.Unix())
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthSession{}, pending.Attempt, ErrAuthMFAChallengeExpired
	}
	if err != nil {
		return AuthSession{}, pending.Attempt, err
	}
	plainState, err := s.openMFA("challenge:"+tokenHash, challenge.EncryptedState)
	if err != nil {
		return AuthSession{}, AuthMFAAttempt{}, err
	}
	if err := json.Unmarshal(plainState, &pending); err != nil {
		return AuthSession{}, AuthMFAAttempt{}, err
	}

	user, secret, err := s.resolveMFAUser(ctx, pending, allowRegistration)
	if err != nil {
		return AuthSession{}, pending.Attempt, err
	}
	step, valid := authTOTPMatchingStep(secret, strings.TrimSpace(code), now)
	if !valid || step <= user.TOTPLastStep {
		recorded, err := s.mfaRepo.RecordFailure(ctx, tokenHash, now.Unix())
		if err != nil {
			return AuthSession{}, pending.Attempt, err
		}
		if !recorded {
			return AuthSession{}, pending.Attempt, ErrAuthMFAChallengeExpired
		}
		return AuthSession{}, pending.Attempt, ErrAuthMFACodeInvalid
	}
	if err := s.completeMFA(ctx, tokenHash, pending, user, secret, step); err != nil {
		return AuthSession{}, pending.Attempt, err
	}
	accessToken, refreshToken, err := s.issueSessionTokens(ctx, user)
	if err != nil {
		return AuthSession{}, pending.Attempt, err
	}
	return AuthSession{
		User:         authUserFromModel(user),
		Token:        accessToken,
		RefreshToken: refreshToken,
	}, pending.Attempt, nil
}

func (s *AuthService) resolveMFAUser(
	ctx context.Context,
	pending authMFAPending,
	allowRegistration func(context.Context) (bool, error),
) (*models.User, string, error) {
	if pending.Attempt.Scene == "register" {
		if allowRegistration != nil {
			allowed, err := allowRegistration(ctx)
			if err != nil {
				return nil, "", err
			}
			if !allowed {
				return nil, "", ErrAuthRegistrationClosed
			}
		}
		return &models.User{
			UserID:       strings.ToLower(ulid.Make().String()),
			Email:        pending.Email,
			Name:         pending.Name,
			PasswordHash: pending.PasswordHash,
			Status:       models.EntityStatusActive,
			TOTPLastStep: -1,
		}, pending.SetupSecret, nil
	}

	user, err := s.userRepo.GetByUserID(ctx, pending.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", ErrAuthMFAChallengeExpired
	}
	if err != nil {
		return nil, "", err
	}
	if err := ensureAuthUserActive(user); err != nil {
		return nil, "", err
	}
	if err := ensureAuthUserLoginIPAllowed(ctx, user); err != nil {
		return nil, "", err
	}
	if hashToken(user.PasswordHash) != pending.PasswordProof || user.MFAGeneration != pending.MFAGeneration {
		return nil, "", ErrAuthMFAChallengeExpired
	}
	if pending.SetupSecret != "" && user.TOTPSecret == "" {
		return user, pending.SetupSecret, nil
	}
	if pending.SetupSecret != "" || user.TOTPSecret == "" {
		return nil, "", ErrAuthMFAChallengeExpired
	}
	secret, err := s.openMFA("user:"+user.UserID, user.TOTPSecret)
	return user, string(secret), err
}

func (s *AuthService) completeMFA(ctx context.Context, tokenHash string, pending authMFAPending, user *models.User, secret string, step int64) error {
	consumed, err := s.mfaRepo.ConsumeChallenge(ctx, tokenHash, s.mfaNow().Unix())
	if err != nil {
		return err
	}
	if !consumed {
		return ErrAuthMFAChallengeExpired
	}
	nextSecret := user.TOTPSecret
	if nextSecret == "" {
		nextSecret, err = s.sealMFA("user:"+user.UserID, []byte(secret))
		if err != nil {
			return err
		}
	}
	if pending.Attempt.Scene == "register" {
		user.TOTPSecret = nextSecret
		user.TOTPLastStep = step
		if err := s.userRepo.Create(ctx, user); err != nil {
			if isUniqueConstraintError(err) {
				return ErrEmailAlreadyExists
			}
			return err
		}
		return nil
	}
	accepted, err := s.mfaRepo.AcceptTOTPStep(ctx, user, nextSecret, step)
	if err != nil {
		return err
	}
	if !accepted {
		return ErrAuthMFAChallengeExpired
	}
	if pending.ExternalProviderID != "" {
		return s.mfaRepo.RecordExternalLogin(ctx, user.UserID, pending.ExternalProviderID, pending.ExternalID, s.mfaNow().UTC())
	}
	return nil
}

func buildAuthTOTPURL(email, secret string) string {
	params := url.Values{"issuer": {config.ProductName}, "secret": {secret}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	u := url.URL{Scheme: "otpauth", Host: "totp", Path: "/" + config.ProductName + ":" + email, RawQuery: params.Encode()}
	return u.String()
}

func authTOTPMatchingStep(secret, code string, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	for _, ch := range code {
		if ch < '0' || ch > '9' {
			return 0, false
		}
	}
	key, err := authTOTPEncoding.DecodeString(secret)
	if err != nil || len(key) != authTOTPSecretSize {
		return 0, false
	}
	current := now.Unix() / authTOTPPeriod
	for _, offset := range [...]int64{0, -1, 1} {
		step := current + offset
		if step >= 0 && subtle.ConstantTimeCompare([]byte(authTOTPCode(key, step)), []byte(code)) == 1 {
			return step, true
		}
	}
	return 0, false
}

func authTOTPCode(key []byte, step int64) string {
	var message [8]byte
	binary.BigEndian.PutUint64(message[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(message[:])
	sum := mac.Sum(nil)
	position := sum[len(sum)-1] & 0x0f
	number := (binary.BigEndian.Uint32(sum[position:position+4]) & 0x7fffffff) % 1000000
	digits := strconv.FormatUint(uint64(number), 10)
	return "000000"[:6-len(digits)] + digits
}

func randomAuthMFABytes(size int) []byte {
	value := make([]byte, size)
	rand.Read(value)
	return value
}

func (s *AuthService) mfaCipher() (cipher.AEAD, error) {
	// Derive a dedicated key so encryption does not reuse the JWT signing key.
	derive := hmac.New(sha256.New, s.jwtSecret)
	derive.Write([]byte("bugodoc/auth/mfa/aead/v1"))
	block, err := aes.NewCipher(derive.Sum(nil))
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func (s *AuthService) sealMFA(purpose string, plaintext []byte) (string, error) {
	aead, err := s.mfaCipher()
	if err != nil {
		return "", err
	}
	nonce := randomAuthMFABytes(aead.NonceSize())
	sealed := aead.Seal(nonce, nonce, plaintext, []byte(purpose))
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *AuthService) openMFA(purpose, encoded string) ([]byte, error) {
	aead, err := s.mfaCipher()
	if err != nil {
		return nil, err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(sealed) < aead.NonceSize()+aead.Overhead() {
		return nil, errors.New("invalid encrypted mfa data")
	}
	nonce := sealed[:aead.NonceSize()]
	plain, err := aead.Open(nil, nonce, sealed[aead.NonceSize():], []byte(purpose))
	if err != nil {
		return nil, fmt.Errorf("open encrypted mfa data: %w", err)
	}
	return plain, nil
}
