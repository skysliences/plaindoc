package handler

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

// ServeImportedOriginal downloads a non-previewable workspace file after checking its current permissions.
func (h *workspaceHandler) ServeImportedOriginal(c *gin.Context) {
	appendVaryHeader(c, "Cookie")
	appendVaryHeader(c, "Authorization")
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Access-Control-Expose-Headers", "Content-Disposition")
	if h == nil || h.visibilityService == nil || h.documentAttachmentRepo == nil {
		writeImportedOriginalDownloadError(c, errors.New("imported original download dependencies are nil"))
		return
	}

	documentID := strings.TrimSpace(c.Param("docId"))
	if documentID == "" {
		response.WorkspaceErrDocumentIDRequired.WriteWithStatus(c)
		return
	}
	viewerID, err := h.resolveOptionalViewerUserID(c)
	if err != nil {
		response.WorkspaceErrAccessToken.Write(c)
		return
	}
	ctx := c.Request.Context()
	if err := h.visibilityService.AuthorizeImportedOriginalDownload(ctx, documentID, viewerID); err != nil {
		writeImportedOriginalDownloadError(c, err)
		return
	}

	attachment, err := h.documentAttachmentRepo.GetByAttachmentID(ctx, documentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.DocumentAttachmentErrAttachmentNotFound.WriteWithStatus(c)
		return
	}
	if err != nil {
		writeImportedOriginalDownloadError(c, err)
		return
	}
	if attachment == nil || attachment.DocumentID != documentID || attachment.Status != models.EntityStatusActive ||
		attachment.StorageProvider != string(service.ImageHostingProviderLocal) || !models.IsImportedOriginalObjectKey(attachment.ObjectKey) {
		response.DocumentAttachmentErrAttachmentNotFound.WriteWithStatus(c)
		return
	}
	targetPath, err := h.resolveLocalAttachmentTargetPath(attachment.ObjectKey)
	if err != nil {
		writeImportedOriginalDownloadError(c, err)
		return
	}
	file, err := os.Open(targetPath)
	if err != nil {
		writeImportedOriginalDownloadError(c, err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeImportedOriginalDownloadError(c, err)
		return
	}
	if !info.Mode().IsRegular() {
		response.DocumentAttachmentErrAttachmentNotFound.WriteWithStatus(c)
		return
	}
	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(attachment.FileName))
	http.ServeContent(c.Writer, c.Request, attachment.FileName, info.ModTime(), file)
	if status := c.Writer.Status(); status == http.StatusOK || status == http.StatusPartialContent {
		recordDocumentActivity(c, h.auditLogRepo, viewerID, documentID, "download")
	}
}

func writeImportedOriginalDownloadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrViewerLoginRequired), errors.Is(err, service.ErrDocumentAccessDenied):
		writeWorkspaceDocumentAccessError(c, err)
	case errors.Is(err, service.ErrDocumentNotFound):
		response.WorkspaceErrDocumentNotFound.WriteWithStatus(c)
	case errors.Is(err, os.ErrNotExist):
		response.DocumentAttachmentErrAttachmentNotFound.WriteWithStatus(c)
	default:
		setRequestErrmsg(c, err, "读取导入原文件失败")
		response.ErrorWithStatus(c, http.StatusInternalServerError, response.CodeInternalError, "服务器内部错误")
	}
}
