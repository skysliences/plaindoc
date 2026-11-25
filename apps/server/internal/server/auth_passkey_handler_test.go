package server

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"
	"github.com/lifei6671/plaindoc/apps/server/internal/logit"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/oklog/ulid/v2"
)

const (
	testPasskeyUserPresent  byte = 1 << 0
	testPasskeyUserVerified byte = 1 << 2
	testPasskeyAttested     byte = 1 << 6
	testPasskeyOrigin            = "http://localhost:5173"
	testPasskeyIP                = "192.0.2.1"
)

type testPasskeyOptions struct {
	ChallengeToken string `json:"challengeToken"`
	PublicKey      struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		AuthenticatorSelection struct {
			ResidentKey      string `json:"residentKey"`
			UserVerification string `json:"userVerification"`
		} `json:"authenticatorSelection"`
		UserVerification string `json:"userVerification"`
	} `json:"publicKey"`
}

type testPasskeyItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
}

type testPasskeySession struct {
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Token        string            `json:"token"`
	RefreshToken string            `json:"refreshToken"`
	MFA          *testMFAChallenge `json:"mfa"`
}

func TestRouter_AuthPasskeyLoginAndManagement(t *testing.T) {
	database, serve := setupPasskeyTestRouter(t)
	const base = "/api/auth/passkeys"
	const password = "passkey-test-password"
	const email = "passkey-test@example.com"
	const verified = testPasskeyUserPresent | testPasskeyUserVerified

	call := func(method, path, token string, input any) *httptest.ResponseRecorder {
		return serve(newTestPasskeyRequest(t, method, testPasskeyOrigin, path, testPasskeyIP, token, input))
	}
	begin := func(path, token string) testPasskeyOptions {
		return readTestPasskeyOptions(t, call(http.MethodPost, path, token, map[string]any{}))
	}
	verify := func(path, token string, options testPasskeyOptions, credential testPasskeyCredential) *httptest.ResponseRecorder {
		return call(http.MethodPost, path, token, map[string]any{
			"challengeToken": options.ChallengeToken,
			"credential":     credential,
		})
	}
	loginOptions := call(http.MethodGet, "/api/auth/options", "", nil)
	requireTestPasskeySuccess(t, loginOptions)
	if !decodeJSONResultData[struct {
		PasskeyEnabled bool `json:"passkeyEnabled"`
	}](t, loginOptions.Body.Bytes()).PasskeyEnabled {
		t.Fatal("expected login options to advertise configured Passkeys")
	}

	anonymousRegistration := call(http.MethodPost, base+"/register/options", "", map[string]any{})
	anonymousList := call(http.MethodGet, base, "", nil)
	for _, rejected := range []*httptest.ResponseRecorder{anonymousRegistration, anonymousList} {
		requireTestPasskeyRejected(t, rejected)
		if rejected.Code != http.StatusUnauthorized {
			t.Fatalf("expected unauthenticated management to return HTTP 401, got %d", rejected.Code)
		}
	}
	register := newTestPasskeyRequest(t, http.MethodPost, testPasskeyOrigin, "/api/auth/register", testPasskeyIP, "", map[string]string{
		"name": "Passkey Test", "email": email, "password": password,
	})
	registration, _ := completeTestMFASetup(t, serve, register)
	account := decodeJSONResultData[testPasskeySession](t, registration.Body.Bytes())
	_, _, otherToken := registerAccessUser(t, serve, "passkey-other@example.com")

	forgedOrigin := newTestPasskeyRequest(t, http.MethodPost, testPasskeyOrigin, base+"/register/options", testPasskeyIP, account.Token, map[string]any{})
	forgedOrigin.Header.Set("Origin", "https://unrelated.example")
	requireTestPasskeyRejected(t, serve(forgedOrigin))

	options := begin(base+"/register/options", account.Token)
	if options.PublicKey.RP.ID != "localhost" || options.PublicKey.User.ID == "" ||
		options.PublicKey.AuthenticatorSelection.ResidentKey != "required" ||
		options.PublicKey.AuthenticatorSelection.UserVerification != "required" {
		t.Fatalf("unexpected registration policy: %+v", options.PublicKey)
	}
	authenticator := newTestPasskeyAuthenticator(t, options.PublicKey.RP.ID, options.PublicKey.User.ID)
	credential := authenticator.registration(t, options.PublicKey.Challenge, testPasskeyOrigin)
	requireTestPasskeyRejected(t, verify(base+"/register/verify", otherToken, options, credential))
	registered := call(http.MethodPost, base+"/register/verify", account.Token, map[string]any{
		"challengeToken": options.ChallengeToken,
		"credential":     credential,
		"name":           "Test security key",
	})
	requireTestPasskeySuccess(t, registered)
	item := decodeJSONResultData[testPasskeyItem](t, registered.Body.Bytes())
	if item.ID == "" || item.Name != "Test security key" || item.CreatedAt == "" {
		t.Fatalf("unexpected registration result: %+v", item)
	}
	requireTestPasskeyRejected(t, verify(base+"/register/verify", account.Token, options, credential))
	requireTestPasskeyRejected(t, call(http.MethodDelete, base+"/"+item.ID, otherToken, nil))

	passwordLogin := call(http.MethodPost, "/api/auth/login", "", map[string]string{
		"identifier": email, "password": password,
	})
	readTestMFAChallenge(t, passwordLogin, "verify")

	assertionOptions := begin(base+"/login/options", "")
	if assertionOptions.PublicKey.RPID != "localhost" || assertionOptions.PublicKey.UserVerification != "required" {
		t.Fatalf("unexpected login policy: %+v", assertionOptions.PublicKey)
	}
	missingUV := authenticator.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, testPasskeyUserPresent, 1)
	requireTestPasskeyRejected(t, verify(base+"/login/verify", "", assertionOptions, missingUV))

	assertionOptions = begin(base+"/login/options", "")
	wrongOrigin := authenticator.assertion(t, assertionOptions.PublicKey.Challenge, "https://unrelated.example", verified, 1)
	requireTestPasskeyRejected(t, verify(base+"/login/verify", "", assertionOptions, wrongOrigin))

	assertionOptions = begin(base+"/login/options", "")
	wrongKey := newTestPasskeyAuthenticator(t, authenticator.rpID, authenticator.userHandle)
	wrongKey.id = authenticator.id
	wrongSignature := wrongKey.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, verified, 1)
	requireTestPasskeyRejected(t, verify(base+"/login/verify", "", assertionOptions, wrongSignature))

	assertionOptions = begin(base+"/login/options", "")
	assertion := authenticator.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, verified, 1)
	loggedIn := verify(base+"/login/verify", "", assertionOptions, assertion)
	requireTestPasskeySuccess(t, loggedIn)
	session := decodeJSONResultData[testPasskeySession](t, loggedIn.Body.Bytes())
	if session.User.ID != account.User.ID || session.Token == "" || session.RefreshToken == "" || session.MFA != nil {
		t.Fatal("expected Passkey login to issue a verified session directly")
	}
	assertSessionCookiesExist(t, loggedIn)
	requireTestPasskeyRejected(t, verify(base+"/login/verify", "", assertionOptions, assertion))
	requireTestPasskeySuccess(t, call(http.MethodGet, "/api/auth/me", session.Token, nil))
	refresh := call(http.MethodPost, "/api/auth/refresh", "", map[string]string{"refreshToken": session.RefreshToken})
	requireTestPasskeySuccess(t, refresh)
	session = decodeJSONResultData[testPasskeySession](t, refresh.Body.Bytes())
	if session.Token == "" || session.RefreshToken == "" {
		t.Fatal("expected refresh of Passkey session to issue replacement tokens")
	}

	result := call(http.MethodGet, base, session.Token, nil)
	requireTestPasskeySuccess(t, result)
	list := decodeJSONResultData[struct {
		Enabled  bool              `json:"enabled"`
		Passkeys []testPasskeyItem `json:"passkeys"`
	}](t, result.Body.Bytes())
	if !list.Enabled || len(list.Passkeys) != 1 || list.Passkeys[0].ID != item.ID || list.Passkeys[0].LastUsedAt == "" {
		t.Fatalf("expected one recently used Passkey: %+v", list)
	}

	if err := database.ORM.Model(&models.User{}).Where(models.UserColumns.UserID+" = ?", account.User.ID).
		Update(models.UserColumns.LoginIPAddressesJSON, `["192.0.2.1"]`).Error; err != nil {
		t.Fatal(err)
	}
	const blockedIP = "198.51.100.15"
	blockedOptions := readTestPasskeyOptions(t, serve(newTestPasskeyRequest(t, http.MethodPost,
		testPasskeyOrigin, base+"/login/options", blockedIP, "", map[string]any{})))
	blockedAssertion := authenticator.assertion(t, blockedOptions.PublicKey.Challenge, testPasskeyOrigin, verified, 2)
	blocked := serve(newTestPasskeyRequest(t, http.MethodPost,
		testPasskeyOrigin, base+"/login/verify", blockedIP, "", map[string]any{
			"challengeToken": blockedOptions.ChallengeToken,
			"credential":     blockedAssertion,
		}))
	requireTestPasskeyRejected(t, blocked)
	if code := decodeJSONResultCode(t, blocked.Body.Bytes()); code != response.ResolveErrorCode(response.CodeLoginIPDenied) {
		t.Fatalf("expected login IP restriction, got business code %d", code)
	}

	requireTestPasskeySuccess(t, call(http.MethodDelete, base+"/"+item.ID, session.Token, nil))
	assertionOptions = begin(base+"/login/options", "")
	assertion = authenticator.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, verified, 2)
	requireTestPasskeyRejected(t, verify(base+"/login/verify", "", assertionOptions, assertion))
}

func TestRouter_AuthPasskeyInvalidatedByMFAReset(t *testing.T) {
	database, serve := setupPasskeyTestRouter(t)
	const base = "/api/auth/passkeys"
	makeRequest := func(method, path, token string, input any) *http.Request {
		return newTestPasskeyRequest(t, method, testPasskeyOrigin, path, testPasskeyIP, token, input)
	}
	adminID, _, adminToken := registerAccessUser(t, serve, "passkey-admin@example.com")
	grantAdminRole(t, database, adminID, "platform_admin")
	userID, _, userToken := registerAccessUser(t, serve, "passkey-reset-target@example.com")

	options := readTestPasskeyOptions(t, serve(makeRequest(http.MethodPost, base+"/register/options", userToken, map[string]any{})))
	authenticator := newTestPasskeyAuthenticator(t, options.PublicKey.RP.ID, options.PublicKey.User.ID)
	credential := authenticator.registration(t, options.PublicKey.Challenge, testPasskeyOrigin)
	requireTestPasskeySuccess(t, serve(makeRequest(http.MethodPost, base+"/register/verify", userToken, map[string]any{
		"challengeToken": options.ChallengeToken, "credential": credential,
	})))

	assertionOptions := readTestPasskeyOptions(t, serve(makeRequest(http.MethodPost, base+"/login/options", "", map[string]any{})))
	reset := makeRequest(http.MethodPost, "/api/admin/users/"+userID+"/reset-mfa", adminToken, map[string]any{})
	attachAdminOperationToken(t, serve, reset, adminToken, "user.reset_mfa", "user", userID)
	requireTestPasskeySuccess(t, serve(reset))

	assertion := authenticator.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, testPasskeyUserPresent|testPasskeyUserVerified, 1)
	requireTestPasskeyRejected(t, serve(makeRequest(http.MethodPost, base+"/login/verify", "", map[string]any{
		"challengeToken": assertionOptions.ChallengeToken, "credential": assertion,
	})))

	passwordLogin := makeRequest(http.MethodPost, "/api/auth/login", "", map[string]string{
		"identifier": "passkey-reset-target@example.com", "password": "123456",
	})
	rebound, _ := completeTestMFASetup(t, serve, passwordLogin)
	session := decodeJSONResultData[testPasskeySession](t, rebound.Body.Bytes())
	if session.User.ID != userID {
		t.Fatal("expected re-enrollment to preserve the account")
	}

	assertionOptions = readTestPasskeyOptions(t, serve(makeRequest(http.MethodPost, base+"/login/options", "", map[string]any{})))
	assertion = authenticator.assertion(t, assertionOptions.PublicKey.Challenge, testPasskeyOrigin, testPasskeyUserPresent|testPasskeyUserVerified, 1)
	requireTestPasskeyRejected(t, serve(makeRequest(http.MethodPost, base+"/login/verify", "", map[string]any{
		"challengeToken": assertionOptions.ChallengeToken, "credential": assertion,
	})))
}

func TestRouter_AuthPasskeyRequiresLocalLoginPolicy(t *testing.T) {
	database, serve := setupPasskeyTestRouter(t)
	userID, _, userToken := registerAccessUser(t, serve, "passkey-ldap-user@example.com")

	// LDAP-managed users retain the same TOTP records but do not have a local password.
	if err := database.ORM.Model(&models.User{}).Where(models.UserColumns.UserID+" = ?", userID).
		Update(models.UserColumns.PasswordHash, "!ldap!").Error; err != nil {
		t.Fatal(err)
	}
	enroll := newTestPasskeyRequest(t, http.MethodPost, testPasskeyOrigin, "/api/auth/passkeys/register/options", testPasskeyIP, userToken, map[string]any{})
	requireTestPasskeyRejected(t, serve(enroll))

	now := time.Now().UTC()
	if err := database.ORM.Create(&models.SystemConfig{
		ConfigKey: "auth",
		ConfigValueJSON: `{
			"loginMode": "ldap_only",
			"defaultProviderId": "corp-ldap",
			"allowUserRegister": false,
			"providers": [{"id": "corp-ldap", "type": "ldap", "enabled": true, "priority": 100}]
		}`,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	options := serve(newTestPasskeyRequest(t, http.MethodGet, testPasskeyOrigin, "/api/auth/options", testPasskeyIP, "", nil))
	requireTestPasskeySuccess(t, options)
	if decodeJSONResultData[struct {
		PasskeyEnabled bool `json:"passkeyEnabled"`
	}](t, options.Body.Bytes()).PasskeyEnabled {
		t.Fatal("LDAP-only policy must not advertise Passkey login")
	}
	begin := newTestPasskeyRequest(t, http.MethodPost, testPasskeyOrigin, "/api/auth/passkeys/login/options", testPasskeyIP, "", map[string]any{})
	requireTestPasskeyRejected(t, serve(begin))
}

func setupPasskeyTestRouter(t *testing.T) (*storage.Database, func(*http.Request) *httptest.ResponseRecorder) {
	t.Helper()

	database, err := storage.OpenDatabase(storage.OpenConfig{
		Driver: storage.DriverSQLite,
		DSN:    "file:test-auth-passkey-" + strings.ToLower(ulid.Make().String()) + "?mode=memory&cache=shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := storage.MigrateUp(context.Background(), database.ORM, storage.DriverSQLite); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig()
	cfg.Auth.PasskeyOrigin = testPasskeyOrigin
	router := NewRouter(cfg, logit.NewLogger(slog.LevelError), database.ORM)
	return database, func(request *http.Request) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, request)
		return recorder
	}
}

func readTestPasskeyOptions(t *testing.T, recorder *httptest.ResponseRecorder) testPasskeyOptions {
	t.Helper()

	requireTestPasskeySuccess(t, recorder)
	options := decodeJSONResultData[testPasskeyOptions](t, recorder.Body.Bytes())
	if options.ChallengeToken == "" || options.PublicKey.Challenge == "" {
		t.Fatal("expected a challenge token and WebAuthn options")
	}
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == "accessToken" || cookie.Name == "refreshToken" {
			t.Fatal("challenge request unexpectedly issued a session")
		}
	}
	return options
}

type testPasskeyAuthenticator struct {
	id         []byte
	key        *ecdsa.PrivateKey
	rpID       string
	userHandle string
}

func newTestPasskeyAuthenticator(t *testing.T, rpID, userHandle string) testPasskeyAuthenticator {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		t.Fatal(err)
	}
	return testPasskeyAuthenticator{id: id, key: key, rpID: rpID, userHandle: userHandle}
}

type testPasskeyCredential struct {
	ID       string            `json:"id"`
	RawID    string            `json:"rawId"`
	Type     string            `json:"type"`
	Response map[string]string `json:"response"`
}

func (a testPasskeyAuthenticator) credential(response map[string]string) testPasskeyCredential {
	id := base64.RawURLEncoding.EncodeToString(a.id)
	return testPasskeyCredential{ID: id, RawID: id, Type: "public-key", Response: response}
}

func (a testPasskeyAuthenticator) registration(t *testing.T, challenge, origin string) testPasskeyCredential {
	t.Helper()

	// COSE EC2 public key using P-256 and ES256.
	publicKey, err := cbor.Marshal(map[int]any{
		1: 2, 3: -7, -1: 1,
		-2: a.key.X.FillBytes(make([]byte, 32)),
		-3: a.key.Y.FillBytes(make([]byte, 32)),
	})
	if err != nil {
		t.Fatal(err)
	}
	authData := a.authenticatorData(testPasskeyUserPresent|testPasskeyUserVerified|testPasskeyAttested, 0)
	authData = append(authData, make([]byte, 16)...)
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(a.id)))
	authData = append(authData, a.id...)
	authData = append(authData, publicKey...)
	attestation, err := cbor.Marshal(map[string]any{
		"fmt": "none", "attStmt": map[string]any{}, "authData": authData,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a.credential(map[string]string{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(testPasskeyClientData(t, "webauthn.create", challenge, origin)),
		"attestationObject": base64.RawURLEncoding.EncodeToString(attestation),
	})
}

func (a testPasskeyAuthenticator) assertion(t *testing.T, challenge, origin string, flags byte, counter uint32) testPasskeyCredential {
	t.Helper()

	clientData := testPasskeyClientData(t, "webauthn.get", challenge, origin)
	authData := a.authenticatorData(flags, counter)
	clientHash := sha256.Sum256(clientData)
	signedData := sha256.New()
	signedData.Write(authData)
	signedData.Write(clientHash[:])
	signature, err := ecdsa.SignASN1(rand.Reader, a.key, signedData.Sum(nil))
	if err != nil {
		t.Fatal(err)
	}
	return a.credential(map[string]string{
		"clientDataJSON":    base64.RawURLEncoding.EncodeToString(clientData),
		"authenticatorData": base64.RawURLEncoding.EncodeToString(authData),
		"signature":         base64.RawURLEncoding.EncodeToString(signature),
		"userHandle":        a.userHandle,
	})
}

func (a testPasskeyAuthenticator) authenticatorData(flags byte, counter uint32) []byte {
	rpIDHash := sha256.Sum256([]byte(a.rpID))
	data := append(rpIDHash[:], flags)
	return binary.BigEndian.AppendUint32(data, counter)
}

func testPasskeyClientData(t *testing.T, ceremony, challenge, origin string) []byte {
	t.Helper()

	data, err := json.Marshal(struct {
		Type      string `json:"type"`
		Challenge string `json:"challenge"`
		Origin    string `json:"origin"`
		CrossSite bool   `json:"crossOrigin"`
	}{Type: ceremony, Challenge: challenge, Origin: origin})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newTestPasskeyRequest(t *testing.T, method, origin, path, ip, token string, input any) *http.Request {
	t.Helper()

	var body []byte
	if input != nil {
		var err error
		body, err = json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
	}
	request := httptest.NewRequest(method, origin+path, bytes.NewReader(body))
	request.RemoteAddr = ip + ":32123"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", origin)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	return request
}

func requireTestPasskeySuccess(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if recorder.Code != http.StatusOK || decodeJSONResultCode(t, recorder.Body.Bytes()) != response.SuccessCode {
		t.Fatalf("expected successful request, got status %d: %s", recorder.Code, recorder.Body.String())
	}
}

func requireTestPasskeyRejected(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()

	if (recorder.Code != http.StatusOK && (recorder.Code < http.StatusBadRequest || recorder.Code >= http.StatusInternalServerError)) ||
		decodeJSONResultCode(t, recorder.Body.Bytes()) == response.SuccessCode {
		t.Fatalf("expected rejected request, got status %d: %s", recorder.Code, recorder.Body.String())
	}
	for _, cookie := range recorder.Result().Cookies() {
		if (cookie.Name == "accessToken" || cookie.Name == "refreshToken") && cookie.Value != "" {
			t.Fatal("rejected request issued a session cookie")
		}
	}
}
