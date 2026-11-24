package server

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_AdminUserLoginIPRestrictions(t *testing.T) {
	t.Setenv("GIN_TRUSTED_PROXIES", "10.0.0.20")
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })
	adminID, _, adminToken := registerAccessUser(t, serve, "login-ip-admin@example.com")
	grantAdminRole(t, database, adminID, "platform_admin")
	register := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader(`{"email":"limited-login-ip@example.com","password":"123456","name":"IP User"}`))
	register.Header.Set("Content-Type", "application/json")
	registered, secret := completeTestMFASetup(t, serve, register)
	registration := decodeJSONResultData[struct {
		User         struct{ ID string }
		Token        string
		RefreshToken string
	}](t, registered.Body.Bytes())
	userID, userToken, refreshToken := registration.User.ID, registration.Token, registration.RefreshToken

	path := "/api/admin/users/" + userID + "/login-ips"
	const body = `{"ipAddresses":["203.0.113.7","2001:0db8::7","::ffff:203.0.113.7"]}`
	update := func(token, operationToken, payload string, status int, code int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Admin-Operation-Token", operationToken)
		rec := serve(req)
		if rec.Code != status {
			t.Fatalf("update login IPs: status %d, want %d; %s", rec.Code, status, rec.Body.String())
		}
		if received := decodeJSONResultCode(t, rec.Body.Bytes()); received != code {
			t.Fatalf("update login IPs: code %d, want %d; %s", received, code, rec.Body.String())
		}
		return rec
	}
	operationToken := func() string {
		return issueAdminOperationToken(t, serve, adminToken, "user.update_login_ips", "user", userID)
	}
	update(userToken, "", body, http.StatusForbidden, response.ResolveErrorCode(response.CodeForbidden))
	update(adminToken, "", body, http.StatusOK, response.ResolveErrorCode(response.CodeOperationTokenRequired))
	update(adminToken, operationToken(), `{"ipAddresses":null}`, http.StatusOK, response.ResolveErrorCode(response.CodeInvalidRequest))
	update(adminToken, operationToken(), `{"ipAddresses":["203.0.113.0/24"]}`, http.StatusOK, response.ResolveErrorCode(response.CodeInvalidRequest))
	updated := decodeJSONResultData[struct{ LoginIPAddresses []string }](t, update(adminToken, operationToken(), body, http.StatusOK, response.SuccessCode).Body.Bytes())
	if want := []string{"2001:db8::7", "203.0.113.7"}; !slices.Equal(updated.LoginIPAddresses, want) {
		t.Fatalf("updated addresses = %v, want %v", updated.LoginIPAddresses, want)
	}
	list := httptest.NewRequest(http.MethodGet, "/api/admin/users?keyword="+userID, nil)
	list.Header.Set("Authorization", "Bearer "+adminToken)
	listResponse := serve(list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list users: %d %s", listResponse.Code, listResponse.Body.String())
	}
	listed := decodeJSONResultData[struct {
		Items []struct{ LoginIPAddresses []string }
	}](t, listResponse.Body.Bytes())
	if len(listed.Items) != 1 || !slices.Equal(listed.Items[0].LoginIPAddresses, updated.LoginIPAddresses) {
		t.Fatalf("listed addresses differ from stored update: %+v", listed)
	}

	me := func(addr, forwarded, cloudflare string, status int) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
		req.RemoteAddr = addr
		req.Header.Set("Authorization", "Bearer "+userToken)
		req.Header.Set("X-Forwarded-For", forwarded)
		req.Header.Set("CF-Connecting-IP", cloudflare)
		rec := serve(req)
		if rec.Code != status {
			t.Fatalf("GET me from %s: status %d, want %d; %s", addr, rec.Code, status, rec.Body.String())
		}
	}
	me("203.0.113.7:1234", "", "", http.StatusOK)
	me("[2001:db8::7]:1234", "", "", http.StatusOK)
	me("198.51.100.41:1234", "203.0.113.7", "203.0.113.7", http.StatusUnauthorized)
	me("10.0.0.20:1234", "203.0.113.7", "", http.StatusOK)
	me("10.0.0.20:1234", "203.0.113.7, 198.51.100.41", "203.0.113.7", http.StatusUnauthorized)

	refresh := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refresh.Header.Set("X-Refresh-Token", refreshToken)
	refresh.RemoteAddr = "198.51.100.41:1234"
	if rec := serve(refresh); rec.Code != http.StatusUnauthorized {
		t.Fatalf("refresh from an unlisted IP: %d %s", rec.Code, rec.Body.String())
	}

	login := func(remoteAddr string) *http.Request {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"email":"limited-login-ip@example.com","password":"123456"}`))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = remoteAddr
		return req
	}
	deniedLogin := login("198.51.100.41:1234")
	if rec := serve(deniedLogin); rec.Code != http.StatusForbidden {
		t.Fatalf("password from an unlisted IP: %d %s", rec.Code, rec.Body.String())
	}
	challenge := readTestMFAChallenge(t, serve(login("203.0.113.7:1234")), "verify")
	code := testMFACode(t, secret, 1)
	if rec := serveTestMFAVerification(t, serve, deniedLogin, challenge.ChallengeToken, code); rec.Code != http.StatusForbidden {
		t.Fatalf("MFA confirmation from an unlisted IP: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveTestMFAVerification(t, serve, login("[2001:db8::7]:1234"), challenge.ChallengeToken, code); rec.Code != http.StatusOK {
		t.Fatalf("MFA confirmation from the second allowed IP: %d %s", rec.Code, rec.Body.String())
	}

	var audits int64
	if err := database.ORM.Model(&models.AuditLog{}).
		Where(models.AuditLogColumns.TargetID+" = ?", userID).
		Where(models.AuditLogColumns.Summary+" LIKE ?", "user login IPs updated:%").
		Count(&audits).Error; err != nil || audits != 1 {
		t.Fatalf("expected one successful update audit, count=%d err=%v", audits, err)
	}
	cleared := decodeJSONResultData[struct{ LoginIPAddresses []string }](t, update(adminToken, operationToken(), `{"ipAddresses":[]}`, http.StatusOK, response.SuccessCode).Body.Bytes())
	if cleared.LoginIPAddresses == nil || len(cleared.LoginIPAddresses) != 0 {
		t.Fatalf("expected array response after clearing, got %#v", cleared.LoginIPAddresses)
	}
	me("198.51.100.41:1234", "", "", http.StatusOK)
}
