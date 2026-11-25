package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/oklog/ulid/v2"
)

type passkeyVerifyRequest struct {
	ChallengeToken string          `json:"challengeToken"`
	Credential     json.RawMessage `json:"credential"`
	Name           string          `json:"name"`
}

type passkeyListResponse struct {
	Enabled  bool                         `json:"enabled"`
	Passkeys []service.AuthPasskeySummary `json:"passkeys"`
}

var passkeyErrors = []response.ErrorTemplateMapping{
	{Target: service.ErrPasskeyInvalid, Template: response.AuthErrPasskeyInvalid},
	{Target: service.ErrPasskeyExpired, Template: response.AuthErrPasskeyExpired},
	{Target: service.ErrPasskeyRecentLogin, Template: response.AuthErrPasskeyRecentLogin},
	{Target: service.ErrPasskeyAlreadyExists, Template: response.AuthErrPasskeyAlreadyExists},
	{Target: service.ErrPasskeyNotFound, Template: response.AuthErrPasskeyNotFound},
	{Target: service.ErrPasskeyLocalAccount, Template: response.AuthErrPasskeyLocalAccount},
	{Target: service.ErrLoginIPDenied, Template: response.AuthErrLoginIPDenied},
	{Target: service.ErrUserBanned, Template: response.AuthErrUserHasBeenBanned},
	{Target: service.ErrUserDeleted, Template: response.AuthErrUserHasBeenDeleted},
}

func (h *authHandler) passkeysAvailable(c *gin.Context) bool {
	origin := c.GetHeader("Origin")
	return h.passkeys != nil && (origin == "" || h.passkeys.AllowsOrigin(origin))
}

func (h *authHandler) requirePasskeyOrigin(c *gin.Context) bool {
	c.Header("Cache-Control", "no-store")
	if !h.passkeys.AllowsOrigin(c.GetHeader("Origin")) {
		response.AuthErrPasskeyUnavailable.Write(c)
		return false
	}
	return true
}

func (h *authHandler) requirePasskeyAccess(c *gin.Context) bool {
	if !h.requirePasskeyOrigin(c) {
		return false
	}
	options, err := h.authRegistrationPolicyService.ResolveLoginOptions(c.Request.Context())
	if err != nil {
		writePasskeyError(c, err)
		return false
	}
	if !options.AllowsLocalLogin() {
		response.AuthErrPasskeyLocalAccount.Write(c)
		return false
	}
	return true
}

func (h *authHandler) BeginPasskeyLogin(c *gin.Context) {
	if !h.requirePasskeyAccess(c) {
		return
	}
	options, err := h.passkeys.BeginLogin(c.Request.Context())
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, options)
}

func (h *authHandler) VerifyPasskeyLogin(c *gin.Context) {
	if !h.requirePasskeyAccess(c) {
		return
	}
	req, ok := bindPasskeyVerification(c)
	if !ok {
		return
	}
	session, err := h.passkeys.FinishLogin(c.Request.Context(), req.ChallengeToken, req.Credential)
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	h.writeAuthSession(c, session)
}

func (h *authHandler) BeginPasskeyRegistration(c *gin.Context) {
	if !h.requirePasskeyAccess(c) {
		return
	}
	token, ok := passkeyAccessToken(c)
	if !ok {
		return
	}
	options, err := h.passkeys.BeginRegistration(c.Request.Context(), token)
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, options)
}

func (h *authHandler) VerifyPasskeyRegistration(c *gin.Context) {
	if !h.requirePasskeyAccess(c) {
		return
	}
	token, ok := passkeyAccessToken(c)
	if !ok {
		return
	}
	req, ok := bindPasskeyVerification(c)
	if !ok {
		return
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > 64 {
		response.AuthErrPasskeyNameTooLong.Write(c)
		return
	}
	if name == "" {
		name = "Passkey"
	}
	passkey, err := h.passkeys.FinishRegistration(c.Request.Context(), token, req.ChallengeToken, name, req.Credential)
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, passkey)
}

func (h *authHandler) ListPasskeys(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	token, ok := passkeyAccessToken(c)
	if !ok {
		return
	}
	if !h.passkeysAvailable(c) {
		if _, err := h.authService.Me(c.Request.Context(), token); err != nil {
			writePasskeyError(c, err)
			return
		}
		response.JSON(c, http.StatusOK, passkeyListResponse{Passkeys: []service.AuthPasskeySummary{}})
		return
	}
	passkeys, err := h.passkeys.List(c.Request.Context(), token)
	if errors.Is(err, service.ErrPasskeyLocalAccount) {
		response.JSON(c, http.StatusOK, passkeyListResponse{Passkeys: []service.AuthPasskeySummary{}})
		return
	}
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	options, err := h.authRegistrationPolicyService.ResolveLoginOptions(c.Request.Context())
	if err != nil {
		writePasskeyError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, passkeyListResponse{Enabled: options.AllowsLocalLogin(), Passkeys: passkeys})
}

func (h *authHandler) DeletePasskey(c *gin.Context) {
	if !h.requirePasskeyOrigin(c) {
		return
	}
	token, ok := passkeyAccessToken(c)
	if !ok {
		return
	}
	id := c.Param("passkeyId")
	if _, err := ulid.ParseStrict(id); err != nil {
		response.AuthErrPasskeyNotFound.Write(c)
		return
	}
	if err := h.passkeys.Delete(c.Request.Context(), token, id); err != nil {
		writePasskeyError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, struct{}{})
}

func passkeyAccessToken(c *gin.Context) (string, bool) {
	token, ok := bearerTokenFromRequest(c)
	if !ok {
		response.AuthErrAccessToken.WriteWithStatus(c)
	}
	return token, ok
}

func bindPasskeyVerification(c *gin.Context) (passkeyVerifyRequest, bool) {
	var req passkeyVerifyRequest
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 256<<10)
	if err := c.ShouldBindJSON(&req); err != nil || req.ChallengeToken == "" || len(req.Credential) == 0 {
		response.AuthErrRequestBody.Write(c)
		return req, false
	}
	return req, true
}

func writePasskeyError(c *gin.Context, err error) {
	if errors.Is(err, service.ErrUnauthorized) {
		response.AuthErrAccessToken.WriteWithStatus(c)
		return
	}
	if errors.Is(err, service.ErrPasskeyBusy) {
		c.Header("Retry-After", "1")
		response.AuthErrPasskeyBusy.WriteWithStatus(c)
		return
	}
	if !response.WriteMappedError(c, err, passkeyErrors...) {
		setRequestErrmsg(c, err, "处理 Passkey 请求失败")
		response.InternalError(c)
	}
}
