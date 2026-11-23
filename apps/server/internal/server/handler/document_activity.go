package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
)

func (h *accessHandler) SetDocumentActivityRepository(repo repository.AuditLogRepository) {
	h.activityRepo = repo
}

func (h *readerPageHandler) SetDocumentActivityRepository(repo repository.AuditLogRepository) {
	h.activityRepo = repo
}

func (h *accessHandler) ReportDocumentCopy(c *gin.Context) {
	if c.ContentType() != "application/json" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "无效的复制事件")
		return
	}
	token, ok := optionalAccessTokenFromRequest(c)
	if !ok {
		response.AccessErrLoginRequired.Write(c)
		return
	}
	actor, err := h.resolveViewerUserIDByToken(c, token)
	if err != nil || actor == "" {
		response.AccessErrLoginRequired.Write(c)
		return
	}
	ctx := c.Request.Context()
	documentID := c.Param("docId")
	if _, err := h.visibilityService.GetDocument(ctx, documentID, actor); err != nil {
		if !response.WriteMappedError(c, err, accessGetDocumentErrorMappings...) {
			response.InternalError(c)
		}
		return
	}
	writeDocumentCopyEvent(c, h.visibilityService, h.activityRepo, actor, documentID)
}

func (h *documentSharePageHandler) ReportDocumentCopy(c *gin.Context) {
	if c.ContentType() != "application/json" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "无效的复制事件")
		return
	}
	viewer := h.readerRenderer.resolveOptionalViewerIdentity(c)
	if !viewer.Authenticated {
		response.AccessErrLoginRequired.Write(c)
		return
	}
	share, err := h.ensureShareAttachmentAccess(c, c.Param("spaceId"), c.Param("docKey"))
	if err != nil {
		if !writeMappedDocumentShareError(c, err) {
			response.InternalError(c)
		}
		return
	}
	writeDocumentCopyEvent(c, h.documentShareService, h.readerRenderer.activityRepo, viewer.UserID, share.DocumentID)
}

type documentCopyPolicy interface {
	DocumentCopyAllowed(ctx context.Context, documentID, userID string) (bool, error)
}

func writeDocumentCopyEvent(c *gin.Context, policy documentCopyPolicy, repo repository.AuditLogRepository, actor, documentID string) {
	ctx := c.Request.Context()
	allowed, err := policy.DocumentCopyAllowed(ctx, documentID, actor)
	if err != nil {
		response.InternalError(c)
		return
	}
	if !allowed {
		response.Error(c, http.StatusForbidden, response.CodeForbidden, "当前文档禁止复制")
		return
	}
	if err := service.RecordDocumentActivity(ctx, repo, actor, documentID, "copy", response.RequestIDFromContext(c)); err != nil {
		setRequestErrmsg(c, err, "记录文档复制失败")
		response.InternalError(c)
		return
	}
	response.JSON(c, http.StatusOK, struct{}{})
}

func recordDocumentActivity(c *gin.Context, repo repository.AuditLogRepository, actor, documentID, action string) {
	if err := service.RecordDocumentActivity(c.Request.Context(), repo, actor, documentID, action, response.RequestIDFromContext(c)); err != nil {
		setRequestErrmsg(c, err, "记录文档活动失败")
	}
}
