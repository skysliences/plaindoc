package handler

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
)

func (h *readerPageHandler) SetDocumentPasswords(passwords *service.DocumentPasswordService) {
	h.documentPasswords = passwords
}

func (h *readerPageHandler) authorizeDocumentPassword(c *gin.Context, documentID, viewerUserID string) (string, error) {
	if h.documentPasswords == nil {
		return "", nil
	}
	return h.documentPasswords.AuthorizedGeneration(
		c.Request.Context(), documentID, viewerUserID, documentUnlockGrant(c, documentID),
	)
}

func writeReaderDocumentPasswordError(c *gin.Context, err error) {
	c.Header("Cache-Control", "private, no-store, max-age=0")
	if errors.Is(err, service.ErrDocumentPasswordRequired) {
		response.ErrorWithStatus(c, http.StatusForbidden, response.CodeDocumentPasswordRequired, "请输入文档查看密码")
		return
	}
	setRequestErrmsg(c, err, "校验文档查看密码失败")
	response.InternalError(c)
}

func readerDocumentPath(spaceID, routeKey string) string {
	return "/r/" + url.PathEscape(spaceID) + "/" + url.PathEscape(routeKey)
}

func (h *readerPageHandler) documentPasswordManagementURL(c *gin.Context, spaceID, documentID, viewerUserID string) string {
	if h.documentPasswords == nil || viewerUserID == "" {
		return ""
	}
	status, err := h.documentPasswords.Status(c.Request.Context(), documentID, viewerUserID)
	if err != nil || !status.CanManage {
		return ""
	}
	return h.webOrigin + "/editor/" + url.PathEscape(spaceID) + "/" + url.PathEscape(documentID) + "?from=reader"
}

// UnlockDocument validates the regular reader permissions before attempting the password.
func (h *readerPageHandler) UnlockDocument(c *gin.Context) {
	spaceID := strings.TrimSpace(c.Param("spaceId"))
	routeKey := strings.TrimSpace(c.Param("docId"))
	viewer := h.resolveOptionalViewerIdentity(c)
	documentID, err := h.readerPageService.ResolveDocumentReadAccess(c.Request.Context(), spaceID, routeKey, viewer.UserID)
	originalFile := false
	if errors.Is(err, service.ErrDocumentNotFound) {
		documentID, err = h.readerPageService.ResolveImportedOriginalDownloadAccess(c.Request.Context(), spaceID, routeKey, viewer.UserID)
		originalFile = err == nil
	}
	switch {
	case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
		h.renderFriendlyErrorPage(c, http.StatusNotFound, "页面不存在", "文档不存在，或已被删除。")
	case errors.Is(err, service.ErrViewerLoginRequired):
		h.renderReaderAccessDeniedPage(c, spaceID, routeKey, viewer, "当前文档需要登录后访问。", true)
	case errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
		h.renderReaderAccessDeniedPage(c, spaceID, routeKey, viewer, "你没有权限访问该文档。", false)
	case err != nil:
		h.logError("resolve reader unlock document failed", err, "document_id", routeKey)
		h.renderFriendlyErrorPage(c, http.StatusInternalServerError, "加载失败", "暂时无法解锁文档，请稍后再试。")
	default:
		pagePath := readerDocumentPath(spaceID, routeKey)
		action := pagePath + "/unlock"
		manageURL := ""
		if originalFile {
			pagePath = action
		} else {
			manageURL = h.documentPasswordManagementURL(c, spaceID, documentID, viewer.UserID)
		}
		h.verifyReaderDocumentUnlock(c, documentID, viewer.UserID, action, pagePath, manageURL)
	}
}

// UnlockDocument preserves the share's own access restrictions before attempting the document password.
func (h *documentSharePageHandler) UnlockDocument(c *gin.Context) {
	spaceID := strings.TrimSpace(c.Param("spaceId"))
	routeKey := strings.TrimSpace(c.Param("docKey"))
	viewer := h.readerRenderer.resolveOptionalViewerIdentity(c)
	share, err := h.resolveAuthorizedShare(c, spaceID, routeKey, viewer.UserID)
	switch {
	case errors.Is(err, service.ErrDocumentShareNotFound):
		h.renderShareFriendlyError(c, http.StatusNotFound, "页面不存在", "分享不存在、已取消或已过期。")
	case errors.Is(err, service.ErrDocumentShareAccessDenied):
		h.renderShareFriendlyError(c, http.StatusForbidden, "没有访问权限", "请先完成该分享的访问验证。")
	case err != nil:
		h.renderShareFriendlyError(c, http.StatusInternalServerError, "加载失败", "暂时无法解锁文档，请稍后再试。")
	default:
		pagePath := buildDocumentSharePagePath(spaceID, resolveDocumentShareCanonicalDocKey(share))
		h.readerRenderer.verifyReaderDocumentUnlock(c, share.DocumentID, viewer.UserID, pagePath+"/unlock", pagePath, "")
	}
}

func (h *readerPageHandler) verifyReaderDocumentUnlock(c *gin.Context, documentID, viewerUserID, action, pagePath, manageURL string) {
	applyReaderBrowserCacheHeaders(c)
	if h.documentPasswords == nil {
		c.Redirect(http.StatusSeeOther, pagePath)
		return
	}
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		h.renderFriendlyErrorPage(c, http.StatusForbidden, "无法解锁", "请打开文档页面后重试。")
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
	if err := c.Request.ParseForm(); err != nil || c.Request.PostForm.Get("password") == "" {
		renderReaderDocumentUnlock(c, http.StatusBadRequest, action, manageURL, "请输入文档查看密码。")
		return
	}

	grant, err := h.documentPasswords.Unlock(c.Request.Context(), documentID, viewerUserID, c.Request.PostForm.Get("password"), c.ClientIP())
	switch {
	case errors.Is(err, service.ErrDocumentPasswordInvalid):
		renderReaderDocumentUnlock(c, http.StatusForbidden, action, manageURL, "密码不正确，请重试。")
	case errors.Is(err, service.ErrDocumentPasswordRateLimited):
		renderReaderDocumentUnlock(c, http.StatusTooManyRequests, action, manageURL, "尝试次数过多，请稍后重试。")
	case err != nil:
		h.logError("verify reader document password failed", err, "document_id", documentID)
		h.renderFriendlyErrorPage(c, http.StatusInternalServerError, "解锁失败", "暂时无法解锁文档，请稍后再试。")
	default:
		if grant != "" {
			writeDocumentUnlockCookie(c, documentID, grant)
		}
		c.Redirect(http.StatusSeeOther, pagePath)
	}
}

// OpenImportedOriginal checks the source download policy and guides readers through any document lock.
func (h *readerPageHandler) OpenImportedOriginal(c *gin.Context) {
	spaceID := strings.TrimSpace(c.Param("spaceId"))
	routeKey := strings.TrimSpace(c.Param("docId"))
	viewer := h.resolveOptionalViewerIdentity(c)
	documentID, err := h.readerPageService.ResolveImportedOriginalDownloadAccess(c.Request.Context(), spaceID, routeKey, viewer.UserID)
	switch {
	case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
		h.renderFriendlyErrorPage(c, http.StatusNotFound, "页面不存在", "原文件不存在，或已被删除。")
		return
	case errors.Is(err, service.ErrViewerLoginRequired), errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
		h.renderFriendlyErrorPage(c, http.StatusForbidden, "没有访问权限", "你没有权限下载该文件。")
		return
	case err != nil:
		h.logError("resolve reader original download failed", err, "document_id", routeKey)
		h.renderFriendlyErrorPage(c, http.StatusInternalServerError, "加载失败", "暂时无法校验原文件下载权限，请稍后再试。")
		return
	}
	applyReaderBrowserCacheHeaders(c)
	if _, err := h.authorizeDocumentPassword(c, documentID, viewer.UserID); err != nil {
		if errors.Is(err, service.ErrDocumentPasswordRequired) {
			renderReaderDocumentUnlock(c, http.StatusOK, readerDocumentPath(spaceID, routeKey)+"/unlock", "", "")
		} else {
			h.logError("reader original document password check failed", err, "document_id", documentID)
			h.renderFriendlyErrorPage(c, http.StatusInternalServerError, "加载失败", "暂时无法校验文档访问权限，请稍后再试。")
		}
		return
	}
	c.Redirect(http.StatusSeeOther, "/api/docs/"+url.PathEscape(documentID)+"/imported-original")
}

var readerDocumentUnlockTemplate = template.Must(template.New("document-unlock").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="robots" content="noindex">
  <title>解锁文档 · bugodoc</title>
  <style>
    :root { color-scheme: light; font-family: "Google Sans Code", "PingFang SC", "Microsoft YaHei", sans-serif; background: #f3f4f6; color: #0f172a; }
    body { box-sizing: border-box; min-height: 100dvh; margin: 0; padding: 20px; display: grid; place-items: center; }
    main { box-sizing: border-box; width: min(100%, 420px); padding: 24px; background: #fff; border: 1px solid #e2e8f0; border-radius: 14px; box-shadow: 0 18px 42px rgba(15, 23, 42, 0.10); }
    h1 { margin: 0 0 16px; font-size: 22px; line-height: 1.35; }
    form { display: grid; gap: 10px; }
    label { font-size: 14px; font-weight: 600; color: #334155; }
    input, button { box-sizing: border-box; width: 100%; min-height: 42px; padding: 8px 12px; border-radius: 8px; font: inherit; }
    input { border: 1px solid #cbd5e1; color: #0f172a; background: #fff; }
    button { border: 1px solid #0f172a; background: #0f172a; color: #fff; font-weight: 600; cursor: pointer; }
    input:focus-visible, button:focus-visible { outline: 2px solid #2563eb; outline-offset: 2px; }
    .manage-password { display: inline-block; margin-top: 16px; font-size: 14px; color: #334155; }
    [role="alert"] { margin: 0; color: #b91c1c; font-size: 14px; line-height: 1.5; }
  </style>
</head>
<body>
  <main aria-labelledby="document-unlock-title">
    <h1 id="document-unlock-title">解锁文档</h1>
    <form method="post" action="{{.Action}}">
      <label for="document-unlock-password">文档查看密码</label>
      <input id="document-unlock-password" name="password" type="password" autocomplete="current-password" required autofocus>
      {{if .Error}}<p role="alert">{{.Error}}</p>{{end}}
      <button type="submit">解锁并查看</button>
    </form>
    {{if .ManageURL}}<a class="manage-password" href="{{.ManageURL}}">管理文档密码</a>{{end}}
  </main>
</body>
</html>`))

func renderReaderDocumentUnlock(c *gin.Context, statusCode int, action, manageURL, errorMessage string) {
	var body bytes.Buffer
	data := struct {
		Action    string
		ManageURL string
		Error     string
	}{Action: action, ManageURL: manageURL, Error: errorMessage}
	if err := readerDocumentUnlockTemplate.Execute(&body, data); err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	applyReaderBrowserCacheHeaders(c)
	c.Header("Referrer-Policy", "no-referrer")
	c.Data(statusCode, "text/html; charset=utf-8", body.Bytes())
}
