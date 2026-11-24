package handler

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/oklog/ulid/v2"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"
	"gorm.io/gorm"
)

const workspaceFolderImageMaxPixels = 40_000_000

type workspaceImportedFileSource struct {
	blob      *models.DocumentAttachmentBlob
	localPath string
}

func detectWorkspaceFolderImageContentType(source workspaceImportedSource) string {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(source.Content))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > workspaceFolderImageMaxPixels/cfg.Height {
		return ""
	}
	var expected string
	switch strings.ToLower(path.Ext(source.SourcePath)) {
	case ".png":
		expected = "png"
	case ".jpg", ".jpeg":
		expected = "jpeg"
	case ".gif":
		expected = "gif"
	case ".webp":
		expected = "webp"
	case ".bmp":
		expected = "bmp"
	}
	if expected != format || expected == "" {
		return ""
	}
	if _, _, err := image.Decode(bytes.NewReader(source.Content)); err != nil {
		return ""
	}
	return "image/" + format
}

func (h *workspaceHandler) stageWorkspaceFolderImage(
	ctx context.Context,
	contentType string,
	content []byte,
	now time.Time,
) (*workspaceImportedFileSource, error) {
	return h.stageWorkspaceImportedFile(ctx, contentType, content, models.ImportedImageObjectPrefix, "."+strings.TrimPrefix(contentType, "image/"), now)
}

func (h *workspaceHandler) stageWorkspaceImportedFile(
	ctx context.Context,
	contentType string,
	content []byte,
	objectPrefix string,
	extension string,
	now time.Time,
) (*workspaceImportedFileSource, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	const storageProvider = service.ImageHostingProviderLocal
	blobID := strings.ToLower(ulid.Make().String())
	// Each imported file has its own private blob, created with the node and attachment.
	hash := sha512.New512_256()
	_, _ = hash.Write([]byte(blobID))
	_, _ = hash.Write(content)
	objectKey := objectPrefix + blobID + extension
	targetPath, err := h.resolveLocalAttachmentTargetPath(objectKey)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(targetPath), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, err
	}
	_, writeErr := file.Write(content)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		return nil, errors.Join(err, os.Remove(targetPath))
	}
	return &workspaceImportedFileSource{
		blob: &models.DocumentAttachmentBlob{
			BlobID:          blobID,
			StorageProvider: string(storageProvider),
			ObjectKey:       objectKey,
			MimeType:        contentType,
			SizeBytes:       int64(len(content)),
			ContentHashAlgo: "sha512-256-with-blob-id",
			ContentHash:     hex.EncodeToString(hash.Sum(nil)),
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		localPath: targetPath,
	}, nil
}

func buildWorkspaceImportedFileAttachment(
	documentID string,
	fileName string,
	previewKind string,
	importState *workspaceImportRuntime,
	blob *models.DocumentAttachmentBlob,
	now time.Time,
) *models.DocumentAttachment {
	return &models.DocumentAttachment{
		// The internal attachment ID matches the document ID for a direct lookup.
		AttachmentID:    documentID,
		BlobID:          blob.BlobID,
		DocumentID:      documentID,
		SpaceID:         importState.spaceID,
		StorageProvider: blob.StorageProvider,
		FileName:        fileName,
		ObjectKey:       blob.ObjectKey,
		MimeType:        blob.MimeType,
		SizeBytes:       blob.SizeBytes,
		ContentHashAlgo: blob.ContentHashAlgo,
		ContentHash:     blob.ContentHash,
		PreviewKind:     previewKind,
		Status:          models.EntityStatusActive,
		CreatedByUserID: &importState.actorUserID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// ServeImportedImage 提供普通空间阅读与编辑器中已导入图片的预览。
func (h *workspaceHandler) ServeImportedImage(c *gin.Context) {
	documentID := strings.TrimSpace(c.Param("docId"))
	viewerUserID, err := h.resolveOptionalViewerUserID(c)
	if err != nil {
		response.WorkspaceErrAccessToken.Write(c)
		return
	}
	document, err := h.visibilityService.GetDocument(c.Request.Context(), documentID, viewerUserID)
	if err != nil {
		writeWorkspaceDocumentAccessError(c, err)
		return
	}
	if document.ImportedFileKind != models.ImportedFileKindImage {
		response.WorkspaceErrDocumentNotFound.Write(c)
		return
	}
	h.serveImportedImage(c, documentID)
}

// ImportedImageHandler 提供通过分享访问授权的导入图片预览。
func (h *documentSharePageHandler) ImportedImageHandler(workspace *workspaceHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		resolved, err := h.ensureShareAttachmentAccess(c, c.Param("spaceId"), c.Param("docKey"))
		if err != nil {
			if !writeMappedDocumentShareError(c, err) {
				response.InternalError(c)
			}
			return
		}
		workspace.serveImportedImage(c, resolved.DocumentID)
	}
}

func (h *workspaceHandler) serveImportedImage(c *gin.Context, documentID string) {
	attachment, err := h.documentAttachmentRepo.GetByAttachmentID(c.Request.Context(), documentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.WorkspaceErrDocumentNotFound.Write(c)
		return
	}
	if err != nil {
		setRequestErrmsg(c, err, "读取导入图片失败")
		response.InternalError(c)
		return
	}
	if attachment.DocumentID != documentID || attachment.Status != models.EntityStatusActive ||
		attachment.StorageProvider != string(service.ImageHostingProviderLocal) ||
		!models.IsImportedImageObjectKey(attachment.ObjectKey) {
		response.WorkspaceErrDocumentNotFound.Write(c)
		return
	}
	switch attachment.MimeType {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/bmp":
	default:
		response.WorkspaceErrDocumentNotFound.Write(c)
		return
	}
	appendVaryHeader(c, "Cookie")
	appendVaryHeader(c, "Authorization")
	c.Header("X-Content-Type-Options", "nosniff")
	h.serveLocalDocumentAttachment(c, attachment, service.DocumentAttachmentLinkPurposePreview)
}
