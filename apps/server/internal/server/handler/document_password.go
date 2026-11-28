package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/pkg/rendercache"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
)

type documentPasswordHandler struct {
	auth      *service.AuthService
	passwords *service.DocumentPasswordService
	cache     *rendercache.Cache
}

func NewDocumentPasswordHandler(auth *service.AuthService, passwords *service.DocumentPasswordService, cache *rendercache.Cache) *documentPasswordHandler {
	return &documentPasswordHandler{auth: auth, passwords: passwords, cache: cache}
}

func (h *workspaceHandler) SetDocumentPasswords(passwords *service.DocumentPasswordService) {
	h.documentPasswords = passwords
}

func (h *documentSharePageHandler) CanReadLockedAsset(c *gin.Context, documentID string) bool {
	config, err := h.documentShareService.GetConfigByDocumentID(c.Request.Context(), documentID)
	if err != nil || config.SpaceID == "" {
		return false
	}
	resolved, err := h.ensureShareAttachmentAccess(c, config.SpaceID, documentID)
	return err == nil && resolved.DocumentID == documentID
}

func (h *documentPasswordHandler) Status(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	viewer, ok := h.viewer(c, false)
	if !ok {
		return
	}
	result, err := h.passwords.Status(c.Request.Context(), c.Param("docId"), viewer)
	if err != nil {
		writeDocumentPasswordError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, result)
}

func (h *documentPasswordHandler) Set(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	viewer, ok := h.viewer(c, true)
	if !ok {
		return
	}
	var payload struct {
		Password json.RawMessage `json:"password"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	var password *string
	if c.ShouldBindJSON(&payload) != nil || len(payload.Password) == 0 || json.Unmarshal(payload.Password, &password) != nil {
		response.AuthErrRequestBody.Write(c)
		return
	}
	documentID := c.Param("docId")
	result, err := h.passwords.Set(c.Request.Context(), documentID, viewer, password)
	if err != nil {
		writeDocumentPasswordError(c, err)
		return
	}
	if h.cache != nil {
		h.cache.PurgeDoc(documentID)
	}
	writeDocumentUnlockCookie(c, documentID, "")
	response.JSON(c, http.StatusOK, result)
}

func (h *documentPasswordHandler) Unlock(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	viewer, ok := h.viewer(c, false)
	if !ok {
		return
	}
	documentID := c.Param("docId")
	ctx := c.Request.Context()
	if err := h.passwords.AuthorizeRead(ctx, documentID, viewer); err != nil {
		writeDocumentPasswordError(c, err)
		return
	}
	var payload struct {
		Password string `json:"password"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if c.ShouldBindJSON(&payload) != nil {
		response.AuthErrRequestBody.Write(c)
		return
	}
	grant, err := h.passwords.Unlock(ctx, documentID, viewer, payload.Password, c.ClientIP())
	if err != nil {
		writeDocumentPasswordError(c, err)
		return
	}
	writeDocumentUnlockCookie(c, documentID, grant)
	response.JSON(c, http.StatusOK, struct{}{})
}

// RequireUnlocked guards content responses while leaving ordinary route authorization in place.
func (h *documentPasswordHandler) RequireUnlocked(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	documentID := c.Param("docId")
	ctx := c.Request.Context()
	generation, err := h.passwords.Generation(ctx, documentID)
	if err != nil {
		writeDocumentPasswordError(c, err)
		return
	}
	if generation != "" {
		viewer, ok := h.viewer(c, false)
		if !ok {
			return
		}
		if err := h.passwords.AuthorizeRead(ctx, documentID, viewer); err != nil {
			writeDocumentPasswordError(c, err)
			return
		}
		if err := h.passwords.Check(ctx, documentID, viewer, documentUnlockGrant(c, documentID)); err != nil {
			writeDocumentPasswordError(c, err)
			return
		}
	}
	c.Next()
}

func (h *documentPasswordHandler) viewer(c *gin.Context, required bool) (string, bool) {
	raw, ok := bearerTokenFromRequest(c)
	if !ok {
		if required {
			response.AuthErrAccessToken.WriteWithStatus(c)
		}
		return "", !required
	}
	session, err := h.auth.Me(c.Request.Context(), raw)
	if err != nil {
		response.AuthErrAccessToken.WriteWithStatus(c)
		return "", false
	}
	return session.User.ID, true
}

func documentUnlockGrant(c *gin.Context, documentID string) string {
	value, _ := c.Cookie(service.DocumentUnlockCookieName(documentID))
	return strings.TrimSpace(value)
}

func writeDocumentUnlockCookie(c *gin.Context, documentID, grant string) {
	maxAge := int(service.DocumentUnlockCookieTTL.Seconds())
	if grant == "" {
		maxAge = -1
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(service.DocumentUnlockCookieName(documentID), grant, maxAge, "/", "", requestUsesHTTPS(c), true)
}

func writeDocumentPasswordError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrDocumentPasswordRequired):
		response.ErrorWithStatus(c, http.StatusForbidden, response.CodeDocumentPasswordRequired, "请输入文档密码后查看")
	case errors.Is(err, service.ErrDocumentPasswordInvalid):
		response.ErrorWithStatus(c, http.StatusBadRequest, response.CodeDocumentPasswordInvalid, "文档密码不正确")
	case errors.Is(err, service.ErrDocumentPasswordRateLimited):
		c.Header("Retry-After", "12")
		response.ErrorWithStatus(c, http.StatusTooManyRequests, response.CodeDocumentPasswordRateLimited, "尝试过于频繁，请稍后重试")
	case errors.Is(err, service.ErrDocumentPasswordLength):
		response.Error(c, http.StatusBadRequest, response.CodeInvalidPassword, "文档密码须为 6 至 72 个 UTF-8 字节")
	default:
		writeWorkspaceDocumentAccessError(c, err)
	}
}
