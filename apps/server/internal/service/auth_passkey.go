package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/lifei6671/plaindoc/apps/server/internal/config"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	"golang.org/x/time/rate"
	"gorm.io/gorm"
)

const (
	passkeyPurposeLogin    = "login"
	passkeyPurposeRegister = "register"
	passkeyChallengeTTL    = 2 * time.Minute
	passkeyEnrollmentAge   = 10 * time.Minute
)

var (
	ErrPasskeyInvalid       = errors.New("invalid passkey proof")
	ErrPasskeyExpired       = errors.New("passkey challenge expired or used")
	ErrPasskeyRecentLogin   = errors.New("passkey enrollment requires a recent sign-in")
	ErrPasskeyAlreadyExists = errors.New("passkey credential already registered")
	ErrPasskeyNotFound      = errors.New("passkey not found")
	ErrPasskeyBusy          = errors.New("too many passkey login challenges")
	ErrPasskeyLocalAccount  = errors.New("passkey requires a local account")
)

type AuthPasskeyService struct {
	auth            *AuthService
	repo            repository.AuthPasskeyRepository
	origin          string
	web             *webauthn.WebAuthn
	loginChallenges *rate.Limiter
}

type AuthPasskeySummary struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"createdAt"`
	LastUsedAt *time.Time `json:"lastUsedAt,omitempty"`
}

type AuthPasskeyOptions[T any] struct {
	ChallengeToken string `json:"challengeToken"`
	PublicKey      T      `json:"publicKey"`
}

type passkeyChallengeState struct {
	Session       webauthn.SessionData `json:"session"`
	AuthSessionID string               `json:"authSessionId,omitempty"`
	MFAGeneration int64                `json:"mfaGeneration,omitempty"`
}

type passkeyUser struct {
	user        *models.User
	credentials webauthn.Credentials
}

func (u passkeyUser) WebAuthnID() []byte                         { return []byte(u.user.UserID) }
func (u passkeyUser) WebAuthnName() string                       { return u.user.Email }
func (u passkeyUser) WebAuthnDisplayName() string                { return u.user.Name }
func (u passkeyUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func NewAuthPasskeyService(auth *AuthService, repo repository.AuthPasskeyRepository, configuredOrigin string) (*AuthPasskeyService, error) {
	origin, err := config.ParsePasskeyOrigin(configuredOrigin)
	if err != nil || origin == nil {
		return nil, err
	}
	web, err := webauthn.New(&webauthn.Config{
		RPID:          origin.Hostname(),
		RPDisplayName: config.ProductName,
		RPOrigins:     []string{origin.String()},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: new(true),
			UserVerification:   protocol.VerificationRequired,
		},
		AttestationPreference: protocol.PreferNoAttestation,
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyChallengeTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: passkeyChallengeTTL},
		},
	})
	if err != nil {
		return nil, err
	}
	return &AuthPasskeyService{
		auth: auth, repo: repo, origin: origin.String(), web: web,
		loginChallenges: rate.NewLimiter(30, 60),
	}, nil
}

func (s *AuthPasskeyService) AllowsOrigin(origin string) bool {
	return s != nil && origin == s.origin
}

func (s *AuthPasskeyService) BeginLogin(ctx context.Context) (AuthPasskeyOptions[protocol.PublicKeyCredentialRequestOptions], error) {
	var result AuthPasskeyOptions[protocol.PublicKeyCredentialRequestOptions]
	if !s.loginChallenges.Allow() {
		return result, ErrPasskeyBusy
	}
	options, session, err := s.web.BeginDiscoverableLogin(webauthn.WithLoginOrigin(s.origin))
	if err != nil {
		return result, err
	}
	result.PublicKey = options.Response
	result.ChallengeToken, err = s.saveChallenge(ctx, passkeyPurposeLogin, parsedToken{}, session)
	return result, err
}

func (s *AuthPasskeyService) FinishLogin(ctx context.Context, challengeToken string, proof []byte) (AuthSession, error) {
	state, err := s.consumeChallenge(ctx, challengeToken, passkeyPurposeLogin, parsedToken{})
	if err != nil {
		return AuthSession{}, err
	}
	assertion, err := protocol.ParseCredentialRequestResponseBytes(proof)
	if err != nil || len(assertion.RawID) == 0 {
		return AuthSession{}, ErrPasskeyInvalid
	}
	record, err := s.repo.GetCredential(ctx, hashToken(string(assertion.RawID)))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthSession{}, ErrPasskeyInvalid
	}
	if err != nil {
		return AuthSession{}, err
	}
	user, err := s.auth.userRepo.GetByUserID(ctx, record.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return AuthSession{}, ErrPasskeyInvalid
	}
	if err != nil {
		return AuthSession{}, err
	}
	if !passkeyLocalAccount(user) || user.TOTPSecret == "" || user.MFAGeneration != record.MFAGeneration {
		return AuthSession{}, ErrPasskeyInvalid
	}
	storedCredential, err := s.openCredential(*record)
	if err != nil {
		return AuthSession{}, err
	}
	_, credential, err := s.web.ValidatePasskeyLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		if !bytes.Equal(rawID, storedCredential.ID) || !bytes.Equal(userHandle, []byte(user.UserID)) {
			return nil, ErrPasskeyInvalid
		}
		return passkeyUser{user: user, credentials: webauthn.Credentials{storedCredential}}, nil
	}, state.Session, assertion)
	if err != nil || (credential.Authenticator.CloneWarning && !credential.Flags.BackupEligible) {
		return AuthSession{}, ErrPasskeyInvalid
	}
	if _, err := primaryAuthIdentity(ctx, user); err != nil {
		return AuthSession{}, err
	}

	encryptedCredential, err := s.sealCredential(*record, credential)
	if err != nil {
		return AuthSession{}, err
	}
	updated, err := s.repo.UpdateCredential(ctx, record.PasskeyID, record.Version, encryptedCredential, time.Now().UTC())
	if err != nil {
		return AuthSession{}, err
	}
	if !updated {
		return AuthSession{}, ErrPasskeyInvalid
	}
	accessToken, refreshToken, err := s.auth.issueSessionTokens(ctx, user)
	if err != nil {
		return AuthSession{}, err
	}
	return AuthSession{User: authUserFromModel(user), Token: accessToken, RefreshToken: refreshToken}, nil
}

func (s *AuthPasskeyService) BeginRegistration(ctx context.Context, accessToken string) (AuthPasskeyOptions[protocol.PublicKeyCredentialCreationOptions], error) {
	var result AuthPasskeyOptions[protocol.PublicKeyCredentialCreationOptions]
	user, token, err := s.sessionUser(ctx, accessToken, true)
	if err != nil {
		return result, err
	}
	records, err := s.repo.ListCredentials(ctx, user.UserID)
	if err != nil {
		return result, err
	}
	credentials := make(webauthn.Credentials, 0, len(records))
	for _, record := range records {
		if record.MFAGeneration != user.MFAGeneration {
			continue
		}
		credential, err := s.openCredential(record)
		if err != nil {
			return result, err
		}
		credentials = append(credentials, credential)
	}
	options, session, err := s.web.BeginRegistration(passkeyUser{user: user, credentials: credentials},
		webauthn.WithExclusions(credentials.CredentialDescriptors()), webauthn.WithRegistrationOrigin(s.origin))
	if err != nil {
		return result, err
	}
	result.PublicKey = options.Response
	result.ChallengeToken, err = s.saveChallenge(ctx, passkeyPurposeRegister, token, session)
	return result, err
}

func (s *AuthPasskeyService) FinishRegistration(ctx context.Context, accessToken, challengeToken, name string, proof []byte) (AuthPasskeySummary, error) {
	user, token, err := s.sessionUser(ctx, accessToken, true)
	if err != nil {
		return AuthPasskeySummary{}, err
	}
	state, err := s.consumeChallenge(ctx, challengeToken, passkeyPurposeRegister, token)
	if err != nil {
		return AuthPasskeySummary{}, err
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(proof)
	if err != nil {
		return AuthPasskeySummary{}, ErrPasskeyInvalid
	}
	credential, err := s.web.CreateCredential(passkeyUser{user: user}, state.Session, parsed)
	if err != nil || len(credential.ID) == 0 {
		return AuthPasskeySummary{}, ErrPasskeyInvalid
	}
	record := models.AuthPasskey{
		PasskeyID:        ulid.Make().String(),
		CredentialIDHash: hashToken(string(credential.ID)),
		UserID:           user.UserID,
		Name:             name,
		MFAGeneration:    user.MFAGeneration,
		CreatedAt:        time.Now().UTC(),
	}
	record.EncryptedCredential, err = s.sealCredential(record, credential)
	if err != nil {
		return AuthPasskeySummary{}, err
	}
	if err := s.repo.CreateCredential(ctx, &record); err != nil {
		if errors.Is(err, gorm.ErrDuplicatedKey) || isUniqueConstraintError(err) {
			return AuthPasskeySummary{}, ErrPasskeyAlreadyExists
		}
		return AuthPasskeySummary{}, err
	}
	return passkeySummary(record), nil
}

func (s *AuthPasskeyService) List(ctx context.Context, accessToken string) ([]AuthPasskeySummary, error) {
	user, _, err := s.sessionUser(ctx, accessToken, false)
	if err != nil {
		return nil, err
	}
	records, err := s.repo.ListCredentials(ctx, user.UserID)
	if err != nil {
		return nil, err
	}
	result := make([]AuthPasskeySummary, 0, len(records))
	for _, record := range records {
		if record.MFAGeneration == user.MFAGeneration {
			result = append(result, passkeySummary(record))
		}
	}
	return result, nil
}

func (s *AuthPasskeyService) Delete(ctx context.Context, accessToken, passkeyID string) error {
	user, _, err := s.sessionUser(ctx, accessToken, false)
	if err != nil {
		return err
	}
	deleted, err := s.repo.DeleteCredential(ctx, user.UserID, passkeyID)
	if err == nil && !deleted {
		return ErrPasskeyNotFound
	}
	return err
}

func passkeySummary(record models.AuthPasskey) AuthPasskeySummary {
	return AuthPasskeySummary{
		ID: record.PasskeyID, Name: record.Name, CreatedAt: record.CreatedAt, LastUsedAt: record.LastUsedAt,
	}
}

func passkeyCredentialPurpose(record models.AuthPasskey) string {
	return strings.Join([]string{"passkey", record.PasskeyID, record.UserID, record.CredentialIDHash,
		strconv.FormatInt(record.MFAGeneration, 10)}, ":")
}

func (s *AuthPasskeyService) openCredential(record models.AuthPasskey) (webauthn.Credential, error) {
	var credential webauthn.Credential
	plain, err := s.auth.openMFA(passkeyCredentialPurpose(record), record.EncryptedCredential)
	if err != nil {
		return credential, err
	}
	err = json.Unmarshal(plain, &credential)
	return credential, err
}

func (s *AuthPasskeyService) sealCredential(record models.AuthPasskey, credential *webauthn.Credential) (string, error) {
	plain, err := json.Marshal(credential)
	if err != nil {
		return "", err
	}
	return s.auth.sealMFA(passkeyCredentialPurpose(record), plain)
}

func (s *AuthPasskeyService) sessionUser(ctx context.Context, accessToken string, recent bool) (*models.User, parsedToken, error) {
	token, err := s.auth.parseToken(accessToken, authTokenTypeAccess)
	if err != nil {
		return nil, token, ErrUnauthorized
	}
	user, err := s.auth.userRepo.GetByUserID(ctx, token.UserID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, token, ErrUnauthorized
	}
	if err != nil {
		return nil, token, err
	}
	if ensureAuthUserActive(user) != nil || user.TOTPSecret == "" || token.MFAGeneration != user.MFAGeneration {
		return nil, token, ErrUnauthorized
	}
	if err := ensureAuthUserLoginIPAllowed(ctx, user); err != nil {
		return nil, token, err
	}
	if !passkeyLocalAccount(user) {
		return nil, token, ErrPasskeyLocalAccount
	}
	now := time.Now().UTC()
	active, err := s.auth.userSessionRepo.IsActive(ctx, token.UserID, token.SessionID, now)
	if err != nil {
		return nil, token, err
	}
	if !active {
		return nil, token, ErrUnauthorized
	}
	if recent && (token.AuthSessionID == "" || token.AuthenticatedAt == 0 || now.Sub(time.Unix(token.AuthenticatedAt, 0)) >= passkeyEnrollmentAge) {
		return nil, token, ErrPasskeyRecentLogin
	}
	return user, token, nil
}

func passkeyLocalAccount(user *models.User) bool {
	hash := strings.TrimSpace(user.PasswordHash)
	return hash != "" && hash != LDAPUserPasswordPlaceholder
}

func (s *AuthPasskeyService) saveChallenge(ctx context.Context, purpose string, token parsedToken, session *webauthn.SessionData) (string, error) {
	state, err := json.Marshal(passkeyChallengeState{
		Session: *session, AuthSessionID: token.AuthSessionID, MFAGeneration: token.MFAGeneration,
	})
	if err != nil {
		return "", err
	}
	random := make([]byte, 32)
	rand.Read(random)
	challengeToken := base64.RawURLEncoding.EncodeToString(random)
	record := models.AuthPasskeyChallenge{
		TokenHash: hashToken(challengeToken), UserID: token.UserID, Purpose: purpose,
		SessionJSON: string(state), ExpiresAt: session.Expires.Unix(),
	}
	return challengeToken, s.repo.SaveChallenge(ctx, &record, time.Now().Unix())
}

func (s *AuthPasskeyService) consumeChallenge(ctx context.Context, challengeToken, purpose string, token parsedToken) (passkeyChallengeState, error) {
	var state passkeyChallengeState
	if len(challengeToken) != base64.RawURLEncoding.EncodedLen(32) {
		return state, ErrPasskeyExpired
	}
	tokenHash := hashToken(challengeToken)
	now := time.Now().Unix()
	record, err := s.repo.GetChallenge(ctx, tokenHash, purpose, token.UserID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return state, ErrPasskeyExpired
	}
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal([]byte(record.SessionJSON), &state); err != nil {
		return state, err
	}
	if state.AuthSessionID != token.AuthSessionID || state.MFAGeneration != token.MFAGeneration || state.Session.Origin != s.origin {
		return state, ErrPasskeyExpired
	}
	consumed, err := s.repo.ConsumeChallenge(ctx, tokenHash, purpose, token.UserID, now)
	if err == nil && !consumed {
		err = ErrPasskeyExpired
	}
	return state, err
}
