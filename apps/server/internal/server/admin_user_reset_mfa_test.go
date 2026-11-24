package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_AdminUserResetMFA(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	adminID, _, adminToken := registerAccessUser(t, serve, "mfa-reset-admin@example.com")
	grantAdminRole(t, database, adminID, "platform_admin")
	spaceAdminID, _, spaceAdminToken := registerAccessUser(t, serve, "mfa-reset-space-admin@example.com")
	grantAdminRole(t, database, spaceAdminID, "space_admin")

	type session struct {
		User         struct{ ID string }
		Token        string
		RefreshToken string
	}
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"mfa-reset-target@example.com","password":"123456","name":"Reset Target"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, oldSecret := completeTestMFASetup(t, serve, register)
	oldSession := decodeJSONResultData[session](t, registered.Body.Bytes())

	loginRequest := func() *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"mfa-reset-target@example.com","password":"123456"}`))
		req.Header.Set("Content-Type", "application/json")
		return req
	}
	assertCode := func(rec *httptest.ResponseRecorder, stage, want string) {
		t.Helper()
		wantCode := response.ResolveErrorCode(want)
		if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != wantCode || rec.Code >= http.StatusInternalServerError {
			t.Fatalf("%s: got HTTP %d and business code %d, want code %d", stage, rec.Code, code, wantCode)
		}
	}
	assertSuccess := func(rec *httptest.ResponseRecorder, stage string) {
		t.Helper()
		if code := decodeJSONResultCode(t, rec.Body.Bytes()); rec.Code != http.StatusOK || code != response.SuccessCode {
			t.Fatalf("%s: got HTTP %d and business code %d, want success", stage, rec.Code, code)
		}
	}
	me := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return serve(req)
	}
	refresh := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
		req.Header.Set("X-Refresh-Token", token)
		return serve(req)
	}
	const requestID = "admin-reset-mfa-test"
	reset := func(token, operationToken string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/users/"+oldSession.User.ID+"/reset-mfa", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Operation-Token", operationToken)
		req.Header.Set("X-Request-Id", requestID)
		return serve(req)
	}

	assertCode(reset(oldSession.Token, ""), "regular user resets MFA", response.CodeForbidden)
	assertCode(reset(spaceAdminToken, ""), "space admin resets MFA", response.CodeForbidden)
	assertCode(reset(adminToken, ""), "reset without operation token", response.CodeOperationTokenRequired)
	wrongTarget := issueAdminOperationToken(t, serve, adminToken, "user.reset_mfa", "user", spaceAdminID)
	assertCode(reset(adminToken, wrongTarget), "reset with another user's operation token", response.CodeOperationTokenScopeMismatch)
	ipUpdate := httptest.NewRequest(http.MethodPut, "/api/admin/users/"+oldSession.User.ID+"/login-ips", strings.NewReader(`{"ipAddresses":["192.0.2.1"]}`))
	ipUpdate.Header.Set("Authorization", "Bearer "+adminToken)
	ipUpdate.Header.Set("Content-Type", "application/json")
	attachAdminOperationToken(t, serve, ipUpdate, adminToken, "user.update_login_ips", "user", oldSession.User.ID)
	assertSuccess(serve(ipUpdate), "set a login IP restriction before reset")
	assertSuccess(me(oldSession.Token), "old access token before reset")

	login := loginRequest()
	pending := readTestMFAChallenge(t, serve(login), "verify")
	operationToken := issueAdminOperationToken(t, serve, adminToken, "user.reset_mfa", "user", oldSession.User.ID)
	assertSuccess(reset(adminToken, operationToken), "platform admin resets MFA")
	assertCode(reset(adminToken, operationToken), "reuse reset operation token", response.CodeOperationTokenReplayed)
	assertCode(me(oldSession.Token), "old access token after reset", response.CodeUnauthorized)
	assertCode(refresh(oldSession.RefreshToken), "old refresh token after reset", response.CodeUnauthorized)
	assertCode(serveTestMFAVerification(t, serve, login, pending.ChallengeToken, testMFACode(t, oldSecret, 1)), "MFA challenge issued before reset", response.CodeAuthMFAChallengeExpired)

	blockedLogin := loginRequest()
	blockedLogin.RemoteAddr = "198.51.100.20:1234"
	assertCode(serve(blockedLogin), "login IP restriction after reset", response.CodeLoginIPDenied)
	login = loginRequest()
	replacement := readTestMFAChallenge(t, serve(login), "setup")
	if replacement.Secret == oldSecret {
		t.Fatal("expected re-enrollment to generate a fresh authenticator key")
	}
	verified, _ := verifyTestMFASetup(t, serve, login, replacement)
	newSession := decodeJSONResultData[session](t, verified.Body.Bytes())
	if newSession.User.ID != oldSession.User.ID {
		t.Fatal("expected authenticator setup to preserve the user's identity")
	}
	assertSuccess(me(newSession.Token), "new access token after re-enrollment")
	assertSuccess(refresh(newSession.RefreshToken), "new refresh token after re-enrollment")
	assertCode(me(oldSession.Token), "old access token after re-enrollment", response.CodeUnauthorized)
	assertCode(refresh(oldSession.RefreshToken), "old refresh token after re-enrollment", response.CodeUnauthorized)
	assertSuccess(me(adminToken), "administrator session after resetting another user")

	var audits []models.AuditLog
	if err := database.ORM.Where(models.AuditLogColumns.RequestID+" = ?", requestID).Find(&audits).Error; err != nil {
		t.Fatalf("find reset audit entry: %v", err)
	}
	if len(audits) != 1 || audits[0].Module != "user" || audits[0].Action != "update" ||
		audits[0].TargetType != "user" || audits[0].TargetID != oldSession.User.ID ||
		audits[0].ActorUserID == nil || *audits[0].ActorUserID != adminID ||
		audits[0].DetailJSON != `{"operation":"user.reset_mfa"}` {
		t.Fatal("expected one MFA reset audit with the target, administrator, and operation")
	}
}
