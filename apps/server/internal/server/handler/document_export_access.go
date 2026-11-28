package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
)

func writeDocumentExportForbidden(c *gin.Context) {
	response.Error(c, http.StatusForbidden, response.CodeForbidden, "你没有权限导出该文档")
}

func (h *readerPageHandler) CheckDocumentExportAccess(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	spaceID := strings.TrimSpace(c.Param("spaceId"))
	documentID := strings.TrimSpace(c.Param("docId"))
	viewer := h.resolveOptionalViewerIdentity(c)
	authorizedDocumentID, allowed, err := h.readerPageService.ResolveDocumentExportAccess(c.Request.Context(), spaceID, documentID, viewer.UserID)
	switch {
	case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
		response.Error(c, http.StatusNotFound, response.CodeDocumentNotFound, "文档不存在，或已被删除")
	case errors.Is(err, service.ErrViewerLoginRequired), errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
		writeDocumentExportForbidden(c)
	case err != nil:
		setRequestErrmsg(c, err, "校验文档导出权限失败")
		response.InternalError(c)
	case !allowed:
		writeDocumentExportForbidden(c)
	default:
		if _, err := h.authorizeDocumentPassword(c, authorizedDocumentID, viewer.UserID); err != nil {
			writeReaderDocumentPasswordError(c, err)
			return
		}
		if c.Request.Method == http.MethodPost {
			recordDocumentActivity(c, h.activityRepo, viewer.UserID, authorizedDocumentID, "download")
		}
		response.JSON(c, http.StatusOK, struct{}{})
	}
}

func (h *documentSharePageHandler) CheckDocumentExportAccess(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	share, err := h.ensureShareAttachmentAccess(c, strings.TrimSpace(c.Param("spaceId")), strings.TrimSpace(c.Param("docKey")))
	if err != nil {
		if !writeMappedDocumentShareError(c, err) {
			response.InternalError(c)
		}
		return
	}
	viewer := h.readerRenderer.resolveOptionalViewerIdentity(c)
	allowed, err := h.documentShareService.DocumentExportAllowed(c.Request.Context(), share.DocumentID, viewer.UserID)
	if err != nil {
		setRequestErrmsg(c, err, "校验分享文档导出权限失败")
		response.InternalError(c)
		return
	}
	if !allowed {
		writeDocumentExportForbidden(c)
		return
	}
	if c.Request.Method == http.MethodPost {
		recordDocumentActivity(c, h.readerRenderer.activityRepo, viewer.UserID, share.DocumentID, "download")
	}
	response.JSON(c, http.StatusOK, struct{}{})
}
