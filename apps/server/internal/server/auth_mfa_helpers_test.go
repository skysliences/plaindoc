package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testMFAChallenge struct {
	ChallengeToken string `json:"challengeToken"`
	Mode           string `json:"mode"`
	Secret         string `json:"secret"`
}

func readTestMFAChallenge(t *testing.T, rec *httptest.ResponseRecorder, mode string) testMFAChallenge {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("expected authentication challenge HTTP 200, got %d", rec.Code)
	}
	if code := decodeJSONResultCode(t, rec.Body.Bytes()); code != 0 {
		t.Fatalf("expected authentication challenge business code 0, got %d", code)
	}
	result := decodeJSONResultData[struct {
		MFA          testMFAChallenge `json:"mfa"`
		Token        string           `json:"token"`
		RefreshToken string           `json:"refreshToken"`
	}](t, rec.Body.Bytes())
	if result.MFA.Mode != mode || result.MFA.ChallengeToken == "" || (mode == "setup") != (result.MFA.Secret != "") {
		t.Fatalf("expected a valid %s MFA challenge", mode)
	}
	if result.Token != "" || result.RefreshToken != "" {
		t.Fatal("primary authentication issued session tokens")
	}
	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name == "accessToken" || cookie.Name == "refreshToken" {
			t.Fatal("primary authentication issued session cookies")
		}
	}
	return result.MFA
}

func testMFACode(t *testing.T, secret string, offset int64) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatal("expected a Base32 authenticator key")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(time.Now().Unix()/30+offset))
	mac := hmac.New(sha1.New, key)
	_, _ = mac.Write(counter[:])
	digest := mac.Sum(nil)
	index := digest[len(digest)-1] & 15
	value := binary.BigEndian.Uint32(digest[index : index+4])
	return fmt.Sprintf("%06d", (value&0x7fffffff)%1000000)
}

func serveTestMFAVerification(
	t *testing.T,
	serve func(*http.Request) *httptest.ResponseRecorder,
	primary *http.Request,
	token string,
	code string,
) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"challengeToken": token, "code": code})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/auth/mfa/verify", bytes.NewReader(body))
	req.RemoteAddr = primary.RemoteAddr
	req.Header = primary.Header.Clone()
	req.Header.Set("Content-Type", "application/json")
	return serve(req)
}

func completeTestMFASetup(
	t *testing.T,
	serve func(*http.Request) *httptest.ResponseRecorder,
	primary *http.Request,
) (*httptest.ResponseRecorder, string) {
	t.Helper()
	return verifyTestMFASetup(t, serve, primary, readTestMFAChallenge(t, serve(primary), "setup"))
}

func verifyTestMFASetup(
	t *testing.T,
	serve func(*http.Request) *httptest.ResponseRecorder,
	primary *http.Request,
	challenge testMFAChallenge,
) (*httptest.ResponseRecorder, string) {
	t.Helper()
	rec := serveTestMFAVerification(t, serve, primary, challenge.ChallengeToken, testMFACode(t, challenge.Secret, 0))
	if rec.Code != http.StatusOK || decodeJSONResultCode(t, rec.Body.Bytes()) != 0 {
		t.Fatalf("expected MFA setup to succeed, got HTTP %d", rec.Code)
	}
	return rec, challenge.Secret
}
