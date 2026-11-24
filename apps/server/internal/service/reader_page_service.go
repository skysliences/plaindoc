package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"gorm.io/gorm"
)

// ReaderPageService 负责聚合阅读页 SSR 所需的数据视图。
type ReaderPageService struct {
	readerPageRepo         repository.ReaderPageRepository
	visibilityService      *VisibilityService
	documentAttachmentRepo repository.DocumentAttachmentRepository
}

type readerTreeNode struct {
	DeleteRestricted            bool
	ImportedOriginalDownloadURL string
	ImportedFileKind            models.ImportedFileKind
	ID                          string
	DocumentID                  *string
	ReaderSlug                  *string
	DocumentPath                *string
	DocumentFormat              *models.DocumentFormat
	ParentID                    *string
	Type                        models.NodeType
	Title                       string
	Sort                        int
	Visibility                  *models.Visibility
	Children                    []*readerTreeNode
}

// NewReaderPageService 创建阅读页聚合服务。
func NewReaderPageService(
	db *gorm.DB,
	visibilityService *VisibilityService,
	documentAttachmentRepo repository.DocumentAttachmentRepository,
) *ReaderPageService {
	return &ReaderPageService{
		readerPageRepo:         repository.NewGormReaderPageRepository(db),
		visibilityService:      visibilityService,
		documentAttachmentRepo: documentAttachmentRepo,
	}
}

// CanCreateDocument reports whether the viewer can create documents in the space.
func (s *ReaderPageService) CanCreateDocument(ctx context.Context, spaceID, viewerUserID string) (bool, error) {
	return s.visibilityService.CanWriteSpace(ctx, spaceID, viewerUserID)
}

func (s *ReaderPageService) CanReviewAllDocuments(ctx context.Context, viewerUserID string) (bool, error) {
	return s.visibilityService.CanReviewAllDocuments(ctx, viewerUserID)
}

// ResolveLandingDocumentID 返回空间阅读入口应跳转的首篇可读文档路由标识（slug 或 document_id）。
func (s *ReaderPageService) ResolveLandingDocumentID(
	ctx context.Context,
	spaceID string,
	viewerUserID string,
) (string, error) {
	if s == nil || s.readerPageRepo == nil || s.visibilityService == nil {
		return "", errors.New("reader page service dependencies are nil")
	}

	normalizedSpaceID := strings.TrimSpace(spaceID)
	normalizedViewerUserID := strings.TrimSpace(viewerUserID)
	if normalizedSpaceID == "" {
		return "", errors.New("space id is required")
	}

	if _, err := s.visibilityService.GetSpace(ctx, normalizedSpaceID, normalizedViewerUserID); err != nil {
		return "", err
	}

	candidateRows, err := s.readerPageRepo.ListTreeNodesBySpaceID(ctx, normalizedSpaceID)
	if err != nil {
		return "", err
	}

	hasDocumentAccessDenied := false
	hasLoginRequired := false
	for _, row := range visibleReaderTreeRows(candidateRows) {
		documentID := normalizeReaderOptionalString(row.DocumentID)
		if documentID == nil || row.ImportedFileKind == models.ImportedFileKindUnsupported {
			continue
		}

		_, readErr := s.visibilityService.GetDocument(ctx, *documentID, normalizedViewerUserID)
		if readErr == nil {
			return resolveReaderDocumentRouteKey(*documentID, row.ReaderSlug), nil
		}

		switch {
		case errors.Is(readErr, ErrViewerLoginRequired):
			hasLoginRequired = true
		case errors.Is(readErr, ErrDocumentAccessDenied):
			hasDocumentAccessDenied = true
		case errors.Is(readErr, ErrDocumentNotFound):
			continue
		default:
			return "", readErr
		}
	}

	if hasLoginRequired {
		return "", ErrViewerLoginRequired
	}
	if hasDocumentAccessDenied {
		return "", ErrDocumentAccessDenied
	}
	return "", ErrDocumentNotFound
}

// ResolveDocumentExportAccess checks read access and export policy for the current reader route.
func (s *ReaderPageService) ResolveDocumentExportAccess(ctx context.Context, spaceID, routeKey, viewerID string) (string, bool, error) {
	documentID, err := s.resolveDocumentID(ctx, spaceID, routeKey)
	if err != nil {
		return "", false, err
	}
	row, err := s.loadDocumentRow(ctx, documentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, ErrDocumentNotFound
	}
	if err != nil {
		return "", false, err
	}
	if row.SpaceID != spaceID {
		return "", false, ErrDocumentNotFound
	}
	if _, err := s.visibilityService.GetDocument(ctx, documentID, viewerID); err != nil {
		return "", false, err
	}
	allowed, err := s.visibilityService.DocumentExportAllowed(ctx, documentID, viewerID)
	return documentID, allowed, err
}

// BuildPage 按 space/doc/viewer 组装阅读页 SSR 输入视图。
func (s *ReaderPageService) BuildPage(
	ctx context.Context,
	spaceID string,
	documentID string,
	viewerUserID string,
) (ReaderPageViewModel, error) {
	if s == nil || s.readerPageRepo == nil || s.visibilityService == nil {
		return ReaderPageViewModel{}, errors.New("reader page service dependencies are nil")
	}

	normalizedSpaceID := strings.TrimSpace(spaceID)
	normalizedDocumentID := strings.TrimSpace(documentID)
	normalizedViewerUserID := strings.TrimSpace(viewerUserID)
	if normalizedSpaceID == "" {
		return ReaderPageViewModel{}, errors.New("space id is required")
	}
	if normalizedDocumentID == "" {
		return ReaderPageViewModel{}, errors.New("document id is required")
	}

	space, err := s.visibilityService.GetSpace(ctx, normalizedSpaceID, normalizedViewerUserID)
	if err != nil {
		return ReaderPageViewModel{}, err
	}

	resolvedDocumentID, err := s.resolveDocumentID(ctx, normalizedSpaceID, normalizedDocumentID)
	if err != nil {
		return ReaderPageViewModel{}, err
	}
	if _, err := s.visibilityService.GetDocument(ctx, resolvedDocumentID, normalizedViewerUserID); err != nil {
		return ReaderPageViewModel{}, err
	}

	documentRow, err := s.loadDocumentRow(ctx, resolvedDocumentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ReaderPageViewModel{}, ErrDocumentNotFound
		}
		return ReaderPageViewModel{}, err
	}
	if strings.TrimSpace(documentRow.SpaceID) != normalizedSpaceID {
		return ReaderPageViewModel{}, ErrDocumentNotFound
	}

	tree, err := s.loadTree(ctx, normalizedSpaceID, normalizedViewerUserID, space)
	if err != nil {
		return ReaderPageViewModel{}, err
	}
	attachments, err := s.loadDocumentAttachments(ctx, resolvedDocumentID)
	if err != nil {
		return ReaderPageViewModel{}, err
	}

	spaceName := strings.TrimSpace(space.Name)
	documentTitle := strings.TrimSpace(documentRow.Title)
	pageTitle := documentTitle
	if pageTitle == "" {
		pageTitle = "未命名文档"
	}
	if spaceName != "" {
		pageTitle = pageTitle + " - " + spaceName
	}

	updatedAt := formatReaderTime(documentRow.UpdatedAt)
	documentIDValue := strings.TrimSpace(documentRow.DocumentID)
	documentIdentifier := normalizeReaderOptionalString(documentRow.ReaderSlug)
	documentRouteKey := resolveReaderDocumentRouteKey(documentIDValue, documentRow.ReaderSlug)

	canCreateDocument, _ := s.CanCreateDocument(ctx, normalizedSpaceID, normalizedViewerUserID)
	copyAllowed, err := s.visibilityService.DocumentCopyAllowed(ctx, documentIDValue, normalizedViewerUserID)
	if err != nil {
		return ReaderPageViewModel{}, err
	}
	exportAllowed, err := s.visibilityService.DocumentExportAllowed(ctx, documentIDValue, normalizedViewerUserID)
	if err != nil {
		return ReaderPageViewModel{}, err
	}

	imageURL := ""
	if documentRow.ImportedFileKind == models.ImportedFileKindImage {
		imageURL = BuildDocumentImportedImageURL(documentIDValue)
	}

	return ReaderPageViewModel{
		CanCreateDocument: canCreateDocument,
		Space: ReaderSpaceViewModel{
			ID:    normalizedSpaceID,
			Name:  spaceName,
			Title: pageTitle,
		},
		Document: ReaderDocumentViewModel{
			ID:               documentIDValue,
			NodeID:           strings.TrimSpace(documentRow.NodeID),
			Identifier:       normalizeReaderString(documentIdentifier),
			RouteKey:         documentRouteKey,
			ThemeID:          strings.TrimSpace(documentRow.ThemeID),
			Format:           models.NormalizeDocumentFormat(documentRow.Format),
			ImportedFileKind: documentRow.ImportedFileKind,
			ImportedImageURL: imageURL,
			Visibility:       normalizeReaderVisibility(documentRow.Visibility),
			Title:            documentTitle,
			ContentMD:        documentRow.ContentMD,
			RenderStatus:     models.NormalizeDocumentRenderStatus(documentRow.RenderStatus),
			RenderError:      strings.TrimSpace(documentRow.RenderError),
			RenderedAt:       formatReaderOptionalTime(documentRow.RenderedAt),
			Version:          documentRow.Version,
			SourceBlobID:     normalizeReaderOptionalString(documentRow.SourceBlobID),
			SourceFileName:   normalizeReaderOptionalString(documentRow.SourceFileName),
			SourceMimeType:   normalizeReaderOptionalString(documentRow.SourceMimeType),
			ContentVersion:   normalizeReaderContentVersion(documentRow.ContentVersion, documentRow.Version),
			AuthorNickname:   normalizeReaderAuthorNickname(documentRow.AuthorNickname),
			UpdatedAt:        updatedAt,
		},
		CopyDenied:   !copyAllowed,
		ExportDenied: !exportAllowed,
		CanEdit: documentRow.ImportedFileKind != models.ImportedFileKindImage &&
			normalizedViewerUserID != "" &&
			s.visibilityService.AuthorizeDocumentWrite(ctx, documentIDValue, normalizedViewerUserID) == nil,
		Attachments: attachments,
		Tree:        tree,
		ActiveDocID: documentIDValue,
	}, nil
}

// BuildSpaceContext 按 space/viewer 组装阅读页左侧所需的空间与目录树上下文。
func (s *ReaderPageService) BuildSpaceContext(
	ctx context.Context,
	spaceID string,
	viewerUserID string,
) (ReaderSpaceViewModel, []ReaderTreeNodeViewModel, error) {
	if s == nil || s.readerPageRepo == nil || s.visibilityService == nil {
		return ReaderSpaceViewModel{}, nil, errors.New("reader page service dependencies are nil")
	}

	normalizedSpaceID := strings.TrimSpace(spaceID)
	normalizedViewerUserID := strings.TrimSpace(viewerUserID)
	if normalizedSpaceID == "" {
		return ReaderSpaceViewModel{}, nil, errors.New("space id is required")
	}

	space, err := s.visibilityService.GetSpace(ctx, normalizedSpaceID, normalizedViewerUserID)
	if err != nil {
		return ReaderSpaceViewModel{}, nil, err
	}

	tree, err := s.loadTree(ctx, normalizedSpaceID, normalizedViewerUserID, space)
	if err != nil {
		return ReaderSpaceViewModel{}, nil, err
	}

	spaceName := strings.TrimSpace(space.Name)
	return ReaderSpaceViewModel{
		ID:    normalizedSpaceID,
		Name:  spaceName,
		Title: spaceName,
	}, tree, nil
}

func (s *ReaderPageService) resolveDocumentID(
	ctx context.Context,
	spaceID string,
	rawDocumentID string,
) (string, error) {
	if s == nil || s.readerPageRepo == nil {
		return "", errors.New("reader page repository is nil")
	}

	resolvedDocumentID, err := s.readerPageRepo.ResolveDocumentID(ctx, spaceID, rawDocumentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrDocumentNotFound
		}
		return "", err
	}
	resolvedDocumentID = strings.TrimSpace(resolvedDocumentID)
	if resolvedDocumentID == "" {
		return "", ErrDocumentNotFound
	}
	return resolvedDocumentID, nil
}

func (s *ReaderPageService) loadDocumentRow(
	ctx context.Context,
	documentID string,
) (repository.ReaderPageDocumentRecord, error) {
	if s == nil || s.readerPageRepo == nil {
		return repository.ReaderPageDocumentRecord{}, errors.New("reader page repository is nil")
	}
	row, err := s.readerPageRepo.GetDocumentByDocumentID(ctx, documentID)
	if err != nil {
		return repository.ReaderPageDocumentRecord{}, err
	}
	if row == nil {
		return repository.ReaderPageDocumentRecord{}, gorm.ErrRecordNotFound
	}
	return *row, nil
}

// A folder deletion includes every descendant, even documents omitted from the reader tree.
func readerDeleteRestrictions(rows []repository.ReaderPageTreeNodeRecord, permissions map[string]models.DocumentPermission) map[string]bool {
	if len(permissions) == 0 {
		return nil
	}
	parents := make(map[string]string, len(rows))
	for _, row := range rows {
		parents[row.NodeID] = normalizeReaderString(row.ParentNodeID)
	}
	restricted := make(map[string]bool)
	for _, row := range rows {
		p := permissions[normalizeReaderString(row.DocumentID)]
		if p.AccessDenied || p.Role == models.RoleReader {
			for id := row.NodeID; id != "" && !restricted[id]; id = parents[id] {
				restricted[id] = true
			}
		}
	}
	return restricted
}

func visibleReaderTreeRows(rows []repository.ReaderPageTreeNodeRecord) []repository.ReaderPageTreeNodeRecord {
	paths := make([]models.WorkspaceNodePathEntry, 0, len(rows))
	for _, row := range rows {
		paths = append(paths, models.WorkspaceNodePathEntry{ID: row.NodeID, ParentID: row.ParentNodeID, Title: row.Title})
	}
	hiddenNodes := models.HiddenWorkspaceNodeIDs(paths)
	visibleRows := rows[:0]
	for _, row := range rows {
		if _, hidden := hiddenNodes[strings.TrimSpace(row.NodeID)]; !hidden {
			visibleRows = append(visibleRows, row)
		}
	}
	return visibleRows
}

func (s *ReaderPageService) loadTree(
	ctx context.Context,
	spaceID, viewerUserID string, originalFileScope *models.Space,
) ([]ReaderTreeNodeViewModel, error) {
	rows, err := s.readerPageRepo.ListTreeNodesBySpaceID(ctx, spaceID)
	if err != nil {
		return nil, err
	}

	permissions, err := s.visibilityService.DocumentPermissionsByViewer(ctx, spaceID, viewerUserID)
	if err != nil {
		return nil, err
	}
	canReview, err := s.CanReviewAllDocuments(ctx, viewerUserID)
	if err != nil {
		return nil, err
	}

	restricted := readerDeleteRestrictions(rows, permissions)
	rows = visibleReaderTreeRows(rows)
	treeNodes := make(map[string]*readerTreeNode, len(rows))
	originalFiles := make(map[string]models.Visibility)
	for _, row := range rows {
		nodeID := strings.TrimSpace(row.NodeID)
		if nodeID == "" {
			continue
		}
		documentID := normalizeReaderOptionalString(row.DocumentID)
		if !canReview && documentID != nil && permissions[*documentID].AccessDenied {
			continue
		}
		readerSlug := normalizeReaderOptionalString(row.ReaderSlug)
		documentPath := resolveReaderTreeDocumentRouteKey(documentID, readerSlug)
		documentVisibility := normalizeReaderDocumentVisibility(row.Type, row.DocumentVisibility)
		documentFormat := normalizeReaderTreeDocumentFormat(row.Type, row.DocumentFormat)
		if row.ImportedFileKind == models.ImportedFileKindUnsupported {
			if originalFileScope != nil && documentID != nil && documentVisibility != nil {
				originalFiles[*documentID] = *documentVisibility
			}
			documentVisibility, documentFormat, readerSlug, documentPath = nil, nil, nil, nil
		}
		treeNodes[nodeID] = &readerTreeNode{
			DeleteRestricted: restricted[nodeID],
			ID:               nodeID,
			DocumentID:       documentID,
			ReaderSlug:       readerSlug,
			DocumentPath:     documentPath,
			DocumentFormat:   documentFormat,
			ImportedFileKind: row.ImportedFileKind,
			ParentID:         normalizeReaderOptionalString(row.ParentNodeID),
			Type:             normalizeReaderNodeType(row.Type),
			Title:            strings.TrimSpace(row.Title),
			Sort:             row.Sort,
			Visibility:       documentVisibility,
			Children:         make([]*readerTreeNode, 0),
		}
	}

	var urls map[string]string
	if originalFileScope != nil {
		urls, err = s.visibilityService.ImportedOriginalDownloadURLs(ctx, spaceID, originalFileScope.OwnerUserID, viewerUserID, originalFileScope.Visibility, originalFiles)
		if err != nil {
			return nil, err
		}
	}
	roots := make([]*readerTreeNode, 0)
	for _, row := range rows {
		nodeID := strings.TrimSpace(row.NodeID)
		node, ok := treeNodes[nodeID]
		if !ok {
			continue
		}
		if node.ImportedFileKind == models.ImportedFileKindUnsupported && node.DocumentID != nil {
			node.ImportedOriginalDownloadURL = urls[*node.DocumentID]
		}
		parentID := normalizeReaderOptionalString(row.ParentNodeID)
		if parentID == nil {
			roots = append(roots, node)
			continue
		}
		parentNode, exists := treeNodes[*parentID]
		if !exists {
			node.ParentID = nil
			roots = append(roots, node)
			continue
		}
		parentNode.Children = append(parentNode.Children, node)
	}

	return mapReaderTreeNodes(roots), nil
}

func normalizeReaderOptionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func normalizeReaderNodeType(value models.NodeType) models.NodeType {
	if value == models.NodeTypeFolder || value == models.NodeTypeDoc {
		return value
	}
	return models.NodeTypeDoc
}

func mapReaderTreeNodes(nodes []*readerTreeNode) []ReaderTreeNodeViewModel {
	items := make([]ReaderTreeNodeViewModel, 0, len(nodes))
	for _, node := range nodes {
		if node == nil {
			continue
		}
		items = append(items, ReaderTreeNodeViewModel{
			DeleteRestricted:            node.DeleteRestricted,
			ImportedOriginalDownloadURL: node.ImportedOriginalDownloadURL,
			ID:                          node.ID,
			DocumentID:                  node.DocumentID,
			DocumentIdentifier:          node.ReaderSlug,
			DocumentRouteKey:            node.DocumentPath,
			DocumentFormat:              node.DocumentFormat,
			ImportedFileKind:            node.ImportedFileKind,
			ParentID:                    node.ParentID,
			Type:                        normalizeReaderNodeType(node.Type),
			Title:                       node.Title,
			Sort:                        node.Sort,
			Visibility:                  node.Visibility,
			Children:                    mapReaderTreeNodes(node.Children),
		})
	}
	return items
}

func resolveReaderDocumentRouteKey(documentID string, readerSlug *string) string {
	normalizedSlug := strings.ToLower(normalizeReaderString(normalizeReaderOptionalString(readerSlug)))
	if normalizedSlug != "" {
		return normalizedSlug
	}
	return strings.TrimSpace(documentID)
}

func resolveReaderTreeDocumentRouteKey(documentID *string, readerSlug *string) *string {
	if documentID == nil {
		return nil
	}
	documentRouteKey := resolveReaderDocumentRouteKey(*documentID, readerSlug)
	if strings.TrimSpace(documentRouteKey) == "" {
		return nil
	}
	return &documentRouteKey
}

func normalizeReaderString(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func (s *ReaderPageService) loadDocumentAttachments(
	ctx context.Context,
	documentID string,
) ([]ReaderDocumentAttachmentViewModel, error) {
	if s == nil || s.documentAttachmentRepo == nil {
		return []ReaderDocumentAttachmentViewModel{}, nil
	}

	normalizedDocumentID := strings.TrimSpace(documentID)
	if normalizedDocumentID == "" {
		return []ReaderDocumentAttachmentViewModel{}, nil
	}

	attachmentRows, err := s.documentAttachmentRepo.ListByDocumentID(ctx, normalizedDocumentID, false)
	if err != nil {
		return nil, err
	}

	attachments := make([]ReaderDocumentAttachmentViewModel, 0, len(attachmentRows))
	for _, item := range attachmentRows {
		if models.IsPrivateImportedAttachmentObjectKey(item.ObjectKey) {
			continue
		}
		attachmentID := strings.TrimSpace(item.AttachmentID)
		if attachmentID == "" {
			continue
		}
		fileName := strings.TrimSpace(item.FileName)
		if fileName == "" {
			fileName = attachmentID
		}
		mimeType := strings.TrimSpace(item.MimeType)
		if mimeType == "" {
			mimeType = "application/octet-stream"
		}
		previewKind := normalizeReaderAttachmentPreviewKind(item.PreviewKind)
		attachments = append(attachments, ReaderDocumentAttachmentViewModel{
			AttachmentID:     attachmentID,
			DocumentID:       strings.TrimSpace(item.DocumentID),
			FileName:         fileName,
			MimeType:         mimeType,
			SizeBytes:        item.SizeBytes,
			PreviewKind:      previewKind,
			PreviewSupported: isReaderAttachmentPreviewSupported(previewKind),
		})
	}
	return attachments, nil
}

func normalizeReaderAttachmentPreviewKind(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "image":
		return "image"
	case "pdf":
		return "pdf"
	case "office":
		return "office"
	case "text":
		return "text"
	default:
		return "none"
	}
}

func isReaderAttachmentPreviewSupported(previewKind string) bool {
	switch normalizeReaderAttachmentPreviewKind(previewKind) {
	case "image", "pdf", "office", "text":
		return true
	default:
		return false
	}
}

func normalizeReaderVisibility(raw string) models.Visibility {
	value := models.Visibility(strings.TrimSpace(raw))
	if !models.IsValidVisibility(value) {
		return models.VisibilityMember
	}
	return value
}

func normalizeReaderDocumentVisibility(
	nodeType models.NodeType,
	rawVisibility *string,
) *models.Visibility {
	if normalizeReaderNodeType(nodeType) != models.NodeTypeDoc {
		return nil
	}
	if rawVisibility == nil {
		visibility := models.VisibilityMember
		return &visibility
	}
	visibility := normalizeReaderVisibility(*rawVisibility)
	return &visibility
}

func normalizeReaderTreeDocumentFormat(
	nodeType models.NodeType,
	rawFormat *models.DocumentFormat,
) *models.DocumentFormat {
	if normalizeReaderNodeType(nodeType) != models.NodeTypeDoc {
		return nil
	}
	if rawFormat == nil {
		format := models.DocumentFormatMarkdown
		return &format
	}
	format := models.NormalizeDocumentFormat(*rawFormat)
	return &format
}

func normalizeReaderContentVersion(contentVersion int, version int) int {
	switch {
	case contentVersion > 0:
		return contentVersion
	case version > 0:
		return version
	default:
		return 1
	}
}

func normalizeReaderAuthorNickname(raw string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "未知作者"
	}
	return name
}

func formatReaderTime(raw time.Time) string {
	if raw.IsZero() {
		return ""
	}
	return raw.UTC().Format(time.RFC3339Nano)
}

func formatReaderOptionalTime(raw *time.Time) string {
	if raw == nil || raw.IsZero() {
		return ""
	}
	return raw.UTC().Format(time.RFC3339Nano)
}
