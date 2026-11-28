package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"gorm.io/gorm"
)

var (
	ErrInvalidVisibilityValue = errors.New("invalid visibility value")
	ErrViewerLoginRequired    = errors.New("viewer login required")
	ErrSpaceAccessDenied      = errors.New("space access denied")
	ErrDocumentAccessDenied   = errors.New("document access denied")
	ErrSpaceNotFound          = errors.New("space not found")
	ErrDocumentNotFound       = errors.New("document not found")
)

// VisibilityService 编排空间/文档公开级别的读取与变更权限判断。
type VisibilityService struct {
	spaceRepo    repository.SpaceRepository
	documentRepo repository.DocumentRepository
	permissions  repository.DocumentPermissionRepository
	adminAccess  *AdminAccessService
}

// NewVisibilityService 创建可见性业务服务。
func NewVisibilityService(
	spaceRepo repository.SpaceRepository,
	documentRepo repository.DocumentRepository,
	permissions repository.DocumentPermissionRepository,
	adminAccess *AdminAccessService,
) *VisibilityService {
	return &VisibilityService{
		spaceRepo:    spaceRepo,
		documentRepo: documentRepo,
		permissions:  permissions,
		adminAccess:  adminAccess,
	}
}

// CanReviewAllDocuments 判断是否可以跨空间查看活跃文档。
func (s *VisibilityService) CanReviewAllDocuments(ctx context.Context, userID string) (bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return false, nil
	}
	return s.adminAccess.IsPlatformAdmin(ctx, userID)
}

// GetSpace 按空间可见性规则返回空间信息。
func (s *VisibilityService) GetSpace(
	ctx context.Context,
	spaceID string,
	viewerUserID string,
) (*models.Space, error) {
	if s == nil || s.spaceRepo == nil {
		return nil, errors.New("visibility service dependencies are nil")
	}

	space, err := s.spaceRepo.GetBySpaceID(ctx, spaceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	if err := EnsureEntityActive(space.Status, space.BannedAt, space.DeletedAt); err != nil {
		return nil, ErrSpaceAccessDenied
	}

	spaceVisibility := normalizeVisibility(space.Visibility)
	if err := s.authorizeRead(ctx, space.SpaceID, spaceVisibility, viewerUserID); err != nil {
		switch {
		case errors.Is(err, ErrViewerLoginRequired):
			return nil, ErrViewerLoginRequired
		case errors.Is(err, ErrSpaceAccessDenied):
			return nil, ErrSpaceAccessDenied
		default:
			return nil, err
		}
	}

	space.Visibility = spaceVisibility
	return space, nil
}

// UpdateSpaceVisibility 仅允许空间 owner 修改空间可见性。
func (s *VisibilityService) UpdateSpaceVisibility(
	ctx context.Context,
	spaceID string,
	actorUserID string,
	visibility models.Visibility,
) (*models.Space, error) {
	if s == nil || s.spaceRepo == nil {
		return nil, errors.New("visibility service dependencies are nil")
	}
	if actorUserID == "" {
		return nil, ErrViewerLoginRequired
	}
	if !models.IsValidVisibility(visibility) {
		return nil, ErrInvalidVisibilityValue
	}

	space, err := s.spaceRepo.GetBySpaceID(ctx, spaceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	if space.OwnerUserID != actorUserID {
		return nil, ErrSpaceAccessDenied
	}
	if err := EnsureEntityActive(space.Status, space.BannedAt, space.DeletedAt); err != nil {
		return nil, ErrSpaceAccessDenied
	}

	updated, err := s.spaceRepo.UpdateVisibility(ctx, spaceID, visibility)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSpaceNotFound
		}
		return nil, err
	}
	updated.Visibility = normalizeVisibility(updated.Visibility)
	return updated, nil
}

// GetDocument 按空间与文档综合可见性规则返回文档信息。
func (s *VisibilityService) GetDocument(
	ctx context.Context,
	documentID string,
	viewerUserID string,
) (*models.Document, error) {
	document, err := s.getDocument(ctx, documentID, viewerUserID, true)
	if err == nil && document.ImportedFileKind == models.ImportedFileKindUnsupported {
		return nil, ErrDocumentNotFound
	}
	return document, err
}

// AuthorizeDocumentAttachmentDownload 保留附件下载的普通阅读权限要求。
func (s *VisibilityService) AuthorizeDocumentAttachmentDownload(ctx context.Context, documentID, viewerUserID string) error {
	_, err := s.getDocument(ctx, documentID, viewerUserID, false)
	return err
}

// AuthorizeImportedOriginalDownload requires both ordinary read access and permission to export the file.
func (s *VisibilityService) AuthorizeImportedOriginalDownload(ctx context.Context, documentID, viewerUserID string) error {
	document, err := s.getDocument(ctx, documentID, viewerUserID, false)
	if err != nil {
		return err
	}
	if document.ImportedFileKind != models.ImportedFileKindUnsupported {
		return ErrDocumentNotFound
	}
	allowed, err := s.DocumentExportAllowed(ctx, documentID, viewerUserID)
	if err == nil && !allowed {
		return ErrDocumentAccessDenied
	}
	return err
}

// ImportedOriginalDownloadURLs applies read and export policy in bulk for directory display.
func (s *VisibilityService) ImportedOriginalDownloadURLs(
	ctx context.Context, spaceID, ownerUserID, viewerUserID string, spaceVisibility models.Visibility,
	documents map[string]models.Visibility,
) (map[string]string, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	urls := make(map[string]string, len(documents))
	if viewerUserID != "" && viewerUserID == ownerUserID {
		for documentID := range documents {
			urls[documentID] = buildImportedOriginalDownloadURL(documentID)
		}
		return urls, nil
	}
	denials, err := s.DocumentExportDenialsByViewer(ctx, spaceID, viewerUserID)
	if err != nil {
		return nil, err
	}
	readByVisibility := make(map[models.Visibility]bool)
	for documentID, documentVisibility := range documents {
		if denials[documentID] {
			continue
		}
		visibility := stricterVisibility(normalizeVisibility(spaceVisibility), normalizeVisibility(documentVisibility))
		allowed, checked := readByVisibility[visibility]
		if !checked {
			err := s.authorizeRead(ctx, spaceID, visibility, viewerUserID)
			if err != nil && !errors.Is(err, ErrViewerLoginRequired) && !errors.Is(err, ErrSpaceAccessDenied) {
				return nil, err
			}
			allowed = err == nil
			readByVisibility[visibility] = allowed
		}
		if allowed {
			urls[documentID] = buildImportedOriginalDownloadURL(documentID)
		}
	}
	return urls, nil
}

func (s *VisibilityService) getDocument(
	ctx context.Context,
	documentID string,
	viewerUserID string,
	allowAdminReview bool,
) (*models.Document, error) {
	if s == nil || s.spaceRepo == nil || s.documentRepo == nil {
		return nil, errors.New("visibility service dependencies are nil")
	}

	documentAccess, err := s.getActiveDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}

	// 空间 owner 恒具备读权限：直接短路返回，避免 member 可见性下的额外成员查询。
	trimmedViewerUserID := strings.TrimSpace(viewerUserID)
	if trimmedViewerUserID != "" && trimmedViewerUserID == strings.TrimSpace(documentAccess.SpaceOwnerUserID) {
		documentAccess.Document.Visibility = normalizeVisibility(documentAccess.Document.Visibility)
		return &documentAccess.Document, nil
	}

	permission, err := s.documentPermission(ctx, documentAccess, trimmedViewerUserID)
	if err != nil {
		return nil, err
	}
	if permission != nil && permission.AccessDenied {
		if !allowAdminReview {
			return nil, ErrDocumentAccessDenied
		}
		canReview, err := s.CanReviewAllDocuments(ctx, trimmedViewerUserID)
		if err != nil {
			return nil, err
		}
		if !canReview {
			return nil, ErrDocumentAccessDenied
		}
	}

	effectiveVisibility := stricterVisibility(
		normalizeVisibility(documentAccess.SpaceVisibility),
		normalizeVisibility(documentAccess.Document.Visibility),
	)
	if err := s.authorizeRead(ctx, documentAccess.SpaceID, effectiveVisibility, trimmedViewerUserID); err != nil {
		switch {
		case errors.Is(err, ErrViewerLoginRequired):
			return nil, ErrViewerLoginRequired
		case errors.Is(err, ErrSpaceAccessDenied):
			return nil, ErrDocumentAccessDenied
		default:
			return nil, err
		}
	}

	documentAccess.Document.Visibility = normalizeVisibility(documentAccess.Document.Visibility)
	return &documentAccess.Document, nil
}

// UpdateDocumentVisibility 允许空间 owner / collaborator / 管理员修改文档可见性。
func (s *VisibilityService) UpdateDocumentVisibility(
	ctx context.Context,
	documentID string,
	actorUserID string,
	visibility models.Visibility,
) (*models.Document, error) {
	if s == nil || s.spaceRepo == nil || s.documentRepo == nil {
		return nil, errors.New("visibility service dependencies are nil")
	}
	if actorUserID == "" {
		return nil, ErrViewerLoginRequired
	}
	if !models.IsValidVisibility(visibility) {
		return nil, ErrInvalidVisibilityValue
	}

	documentAccess, err := s.getActiveDocument(ctx, documentID)
	if err != nil {
		return nil, err
	}
	if err := s.authorizeDocumentWrite(ctx, documentAccess, actorUserID); err != nil {
		return nil, err
	}

	if strings.TrimSpace(documentAccess.SpaceOwnerUserID) != strings.TrimSpace(actorUserID) {
		hasWriterAccess, accessErr := s.spaceRepo.HasWriterAccess(ctx, documentAccess.SpaceID, actorUserID)
		if accessErr != nil {
			return nil, accessErr
		}
		if !hasWriterAccess {
			return nil, ErrDocumentAccessDenied
		}
	}

	updated, err := s.documentRepo.UpdateVisibility(ctx, documentID, visibility)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDocumentNotFound
		}
		return nil, err
	}
	updated.Visibility = normalizeVisibility(updated.Visibility)
	return updated, nil
}

func (s *VisibilityService) getActiveDocument(
	ctx context.Context, documentID string,
) (*repository.DocumentAccessInfo, error) {
	documentAccess, err := s.documentRepo.GetAccessByDocumentID(ctx, documentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDocumentNotFound
		}
		return nil, err
	}
	if err := EnsureEntityActive(
		documentAccess.SpaceStatus,
		documentAccess.SpaceBannedAt,
		documentAccess.SpaceDeletedAt,
	); err != nil {
		return nil, ErrDocumentAccessDenied
	}
	if err := EnsureEntityActive(
		documentAccess.Document.Status,
		documentAccess.Document.BannedAt,
		documentAccess.Document.DeletedAt,
	); err != nil {
		return nil, ErrDocumentAccessDenied
	}

	return documentAccess, nil
}

func (s *VisibilityService) documentPermission(
	ctx context.Context, document *repository.DocumentAccessInfo, userID string,
) (*models.DocumentPermission, error) {
	if userID == "" || userID == document.SpaceOwnerUserID {
		return nil, nil
	}
	rows, err := s.permissions.List(ctx, document.SpaceID, document.Document.DocumentID, userID)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return &rows[0], nil
}

func (s *VisibilityService) CanWriteSpace(ctx context.Context, spaceID, userID string) (bool, error) {
	return s.spaceRepo.HasWriterAccess(ctx, spaceID, userID)
}

// CanUploadImage 支持文档级编辑权限，保留没有现存文档时的空间上传入口。
func (s *VisibilityService) CanUploadImage(ctx context.Context, spaceID, documentID, userID string) (bool, error) {
	if documentID != "" {
		document, err := s.getActiveDocument(ctx, documentID)
		if err == nil {
			if document.SpaceID != spaceID {
				return false, nil
			}
			err = s.authorizeDocumentWrite(ctx, document, userID)
			if errors.Is(err, ErrDocumentAccessDenied) {
				return false, nil
			}
			return err == nil, err
		}
		if errors.Is(err, ErrDocumentAccessDenied) {
			return false, nil
		}
		if !errors.Is(err, ErrDocumentNotFound) {
			return false, err
		}
	}
	return s.CanWriteSpace(ctx, spaceID, userID)
}

func (s *VisibilityService) IsDocumentExplicitlyDenied(ctx context.Context, documentID, userID string) (bool, error) {
	if userID == "" {
		return false, nil
	}
	rows, err := s.permissions.List(ctx, "", documentID, userID)
	return len(rows) > 0 && rows[0].AccessDenied, err
}

// DocumentCopyAllowed resolves clipboard policy for a viewer after document access has been checked.
func (s *VisibilityService) DocumentCopyAllowed(ctx context.Context, documentID, userID string) (bool, error) {
	if userID == "" {
		return true, nil
	}
	document, err := s.getActiveDocument(ctx, documentID)
	if err != nil {
		return false, err
	}
	permission, err := s.documentPermission(ctx, document, userID)
	return err == nil && (permission == nil || (!permission.CopyDenied && !permission.AccessDenied)), err
}

// DocumentExportAllowed resolves the viewer's export policy after read authorization.
func (s *VisibilityService) DocumentExportAllowed(ctx context.Context, documentID, userID string) (bool, error) {
	if userID == "" {
		return true, nil
	}
	document, err := s.getActiveDocument(ctx, documentID)
	if err != nil {
		return false, err
	}
	permission, err := s.documentPermission(ctx, document, userID)
	return err == nil && (permission == nil || (!permission.ExportDenied && !permission.AccessDenied)), err
}

// DocumentExportDenialsByViewer returns the documents excluded by the viewer's
// explicit export or access restrictions in an already authorized space.
func (s *VisibilityService) DocumentExportDenialsByViewer(
	ctx context.Context, spaceID, userID string,
) (map[string]bool, error) {
	denials := make(map[string]bool)
	if userID == "" {
		return denials, nil
	}
	rows, err := s.permissions.List(ctx, spaceID, "", userID)
	if err != nil {
		return nil, err
	}
	for _, permission := range rows {
		if permission.ExportDenied || permission.AccessDenied {
			denials[permission.DocumentID] = true
		}
	}
	return denials, nil
}

func (s *VisibilityService) AuthorizeDocumentWrite(ctx context.Context, documentID, userID string) error {
	if userID == "" {
		return ErrViewerLoginRequired
	}
	document, err := s.getActiveDocument(ctx, documentID)
	if err != nil {
		return err
	}
	return s.authorizeDocumentWrite(ctx, document, userID)
}

func (s *VisibilityService) authorizeDocumentWrite(ctx context.Context, document *repository.DocumentAccessInfo, userID string) error {
	if document.Document.ImportedFileKind == models.ImportedFileKindUnsupported {
		return ErrDocumentNotFound
	}
	return s.authorizeDocumentWriteAccess(ctx, document, userID)
}

func (s *VisibilityService) authorizeDocumentWriteAccess(ctx context.Context, document *repository.DocumentAccessInfo, userID string) error {
	if userID == document.SpaceOwnerUserID {
		return nil
	}
	permission, err := s.documentPermission(ctx, document, userID)
	if err != nil {
		return err
	}
	if permission != nil && !permission.InheritAccess {
		if permission.AccessDenied || permission.Role == models.RoleReader {
			return ErrDocumentAccessDenied
		}
		member, err := s.spaceRepo.IsMember(ctx, document.SpaceID, userID)
		if err != nil || member {
			return err
		}
	}

	canWrite, err := s.spaceRepo.HasWriterAccess(ctx, document.SpaceID, userID)
	if err != nil {
		return err
	}
	if !canWrite {
		return ErrDocumentAccessDenied
	}
	return nil
}

func (s *VisibilityService) DocumentPermissionsByViewer(
	ctx context.Context, spaceID, userID string,
) (map[string]models.DocumentPermission, error) {
	result := make(map[string]models.DocumentPermission)
	if userID == "" {
		return result, nil
	}
	rows, err := s.permissions.List(ctx, spaceID, "", userID)
	for _, permission := range rows {
		if !permission.InheritAccess {
			result[permission.DocumentID] = permission
		}
	}
	return result, err
}

func (s *VisibilityService) authorizeRead(
	ctx context.Context,
	spaceID string,
	visibility models.Visibility,
	viewerUserID string,
) error {
	switch visibility {
	case models.VisibilityPublic:
		return nil
	case models.VisibilityAuthenticated:
		if viewerUserID == "" {
			return ErrViewerLoginRequired
		}
		return nil
	case models.VisibilityMember:
		if viewerUserID == "" {
			return ErrViewerLoginRequired
		}
		member, err := s.spaceRepo.HasReaderAccess(ctx, spaceID, viewerUserID)
		if err != nil {
			return err
		}
		if !member {
			return ErrSpaceAccessDenied
		}
		return nil
	default:
		return fmt.Errorf("unsupported visibility %q", visibility)
	}
}

func normalizeVisibility(value models.Visibility) models.Visibility {
	if models.IsValidVisibility(value) {
		return value
	}
	return models.VisibilityMember
}

func stricterVisibility(left models.Visibility, right models.Visibility) models.Visibility {
	if visibilityRank(left) >= visibilityRank(right) {
		return left
	}
	return right
}

func visibilityRank(value models.Visibility) int {
	switch value {
	case models.VisibilityPublic:
		return 1
	case models.VisibilityAuthenticated:
		return 2
	case models.VisibilityMember:
		return 3
	default:
		return 3
	}
}
