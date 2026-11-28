package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/config"
	"github.com/lifei6671/plaindoc/apps/server/internal/pkg/rendercache"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/ssr/pool"
	"github.com/lifei6671/plaindoc/apps/server/internal/ssr/protocol"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
)

type readerPageHandler struct {
	activityRepo                 repository.AuditLogRepository
	authService                  *service.AuthService
	documentPasswords            *service.DocumentPasswordService
	readerPageService            *service.ReaderPageService
	dispatcher                   *pool.Dispatcher
	renderCache                  *rendercache.Cache
	logger                       *slog.Logger
	webOrigin                    string
	onlyOfficeConfigService      *service.OnlyOfficeConfigService
	officeRenderingConfigService *service.OfficeRenderingConfigService
	officeReaderPreview          *service.OfficeReaderPreviewService
	onlyOfficeTokenService       *service.OnlyOfficeDocumentTokenService
	searchConfig                 *service.SearchConfigService
}

type readerPageViewerIdentity struct {
	UserID        string `json:"userId,omitempty"`
	Name          string `json:"name,omitempty"`
	Authenticated bool   `json:"authenticated"`
}

type readerPagePayload struct {
	ExportDenied          bool                                        `json:"exportDenied,omitempty"`
	CopyDenied            bool                                        `json:"copyDenied,omitempty"`
	TrackDocumentActivity bool                                        `json:"trackDocumentActivity,omitempty"`
	SearchEnabled         bool                                        `json:"searchEnabled,omitempty"`
	CreateDocumentURL     string                                      `json:"createDocumentUrl,omitempty"`
	EditURL               string                                      `json:"editUrl,omitempty"`
	Space                 service.ReaderSpaceViewModel                `json:"space"`
	Document              service.ReaderDocumentViewModel             `json:"document"`
	Attachments           []service.ReaderDocumentAttachmentViewModel `json:"attachments"`
	Tree                  []service.ReaderTreeNodeViewModel           `json:"tree"`
	ActiveDocID           string                                      `json:"activeDocId"`
	RequestOrigin         string                                      `json:"requestOrigin,omitempty"`
	Viewer                readerPageViewerIdentity                    `json:"viewer"`
	OfficeRendering       readerPageOfficeRenderingState              `json:"officeRendering"`
	localOfficeFallback   bool
	Access                *readerPageAccessState `json:"access,omitempty"`
	Share                 *readerPageShareState  `json:"share,omitempty"`
}

type readerPageAccessState struct {
	Code          string `json:"code"`
	Title         string `json:"title"`
	Description   string `json:"description"`
	RequiresLogin bool   `json:"requiresLogin"`
}

type readerPageShareState struct {
	Enabled            bool   `json:"enabled"`
	ShareID            string `json:"shareId,omitempty"`
	SpaceID            string `json:"spaceId,omitempty"`
	DocumentRouteKey   string `json:"documentRouteKey,omitempty"`
	BasePath           string `json:"basePath,omitempty"`
	AttachmentBasePath string `json:"attachmentBasePath,omitempty"`
}

type readerPageOfficeRenderingState struct {
	IndependentRenderEnabled            bool `json:"independentRenderEnabled"`
	FallbackToOnlyOfficeOnRenderFailure bool `json:"fallbackToOnlyOfficeOnRenderFailure"`
}

// NewReaderPageHandler 创建阅读页 SSR 处理器。
func NewReaderPageHandler(
	authService *service.AuthService,
	readerPageService *service.ReaderPageService,
	dispatcher *pool.Dispatcher,
	renderCache *rendercache.Cache,
	logger *slog.Logger,
	webOrigin string,
	onlyOfficeConfigService *service.OnlyOfficeConfigService,
	officeRenderingConfigService *service.OfficeRenderingConfigService,
	officeReaderPreview *service.OfficeReaderPreviewService,
	onlyOfficeTokenService *service.OnlyOfficeDocumentTokenService,
	searchConfig *service.SearchConfigService,
) *readerPageHandler {
	return &readerPageHandler{
		authService:                  authService,
		readerPageService:            readerPageService,
		dispatcher:                   dispatcher,
		renderCache:                  renderCache,
		logger:                       logger,
		webOrigin:                    normalizeWebOrigin(webOrigin),
		onlyOfficeConfigService:      onlyOfficeConfigService,
		officeRenderingConfigService: officeRenderingConfigService,
		officeReaderPreview:          officeReaderPreview,
		onlyOfficeTokenService:       onlyOfficeTokenService,
		searchConfig:                 searchConfig,
	}
}

// Space 将访客引导到空间内首篇可读文档。
func (h *readerPageHandler) Space(c *gin.Context) {
	if h == nil || h.readerPageService == nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(readerFallbackUnavailableHTML))
		return
	}

	spaceID := strings.TrimSpace(c.Param("spaceId"))
	if spaceID == "" {
		h.renderFriendlyErrorPage(
			c,
			http.StatusBadRequest,
			"链接无效",
			"空间标识不正确，请检查链接后重试。",
		)
		return
	}

	appendVaryHeader(c, "Authorization")
	appendVaryHeader(c, "Cookie")
	c.Header("Cache-Control", "private, no-store, max-age=0")

	viewer := h.resolveOptionalViewerIdentity(c)
	documentID, err := h.readerPageService.ResolveLandingDocumentID(
		c.Request.Context(),
		spaceID,
		viewer.UserID,
	)
	if err != nil {
		if errors.Is(err, service.ErrDocumentNotFound) || errors.Is(err, service.ErrDocumentAccessDenied) {
			canReview, roleErr := h.readerPageService.CanReviewAllDocuments(c.Request.Context(), viewer.UserID)
			if roleErr != nil {
				h.logError("resolve reader review role failed", roleErr, "space_id", spaceID)
			} else if h.renderReaderSpaceDirectory(c, spaceID, viewer, !canReview) {
				return
			}
		}
		if viewer.Authenticated && errors.Is(err, service.ErrDocumentNotFound) {
			canCreate, permissionErr := h.readerPageService.CanCreateDocument(c.Request.Context(), spaceID, viewer.UserID)
			if permissionErr == nil && canCreate {
				c.Redirect(http.StatusSeeOther, h.webOrigin+"/editor/"+url.PathEscape(spaceID)+"?from=reader&create=document")
				return
			}
		}
		switch {
		case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
			h.renderFriendlyErrorPage(
				c,
				http.StatusNotFound,
				"页面不存在",
				"空间不存在，或该空间下暂无可访问文档。",
			)
		case errors.Is(err, service.ErrViewerLoginRequired):
			h.renderFriendlyErrorPage(
				c,
				http.StatusForbidden,
				"无权限访问",
				"当前空间需要登录后访问，且本页面不会自动跳转登录。",
			)
		case errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
			h.renderFriendlyErrorPage(
				c,
				http.StatusForbidden,
				"无权限访问",
				"你没有权限访问该空间或文档，请联系空间管理员。",
			)
		default:
			h.logError("resolve reader landing document failed", err, "space_id", spaceID)
			h.renderFriendlyErrorPage(
				c,
				http.StatusInternalServerError,
				"加载失败",
				"阅读页面暂时不可用，请稍后刷新重试。",
			)
		}
		return
	}

	targetPath := "/r/" + url.PathEscape(spaceID) + "/" + url.PathEscape(documentID)
	c.Redirect(http.StatusSeeOther, targetPath)
}

func (h *readerPageHandler) renderReaderSpaceDirectory(c *gin.Context, spaceID string, viewer readerPageViewerIdentity, requireImportedFiles bool) bool {
	space, tree, err := h.readerPageService.BuildSpaceContext(c.Request.Context(), spaceID, viewer.UserID)
	if err != nil {
		h.logError("build reader space directory failed", err, "space_id", spaceID)
		return false
	}
	if requireImportedFiles && !hasReaderUnsupportedFiles(tree) {
		return false
	}
	canCreate, _ := h.readerPageService.CanCreateDocument(c.Request.Context(), spaceID, viewer.UserID)
	payload := readerPagePayload{
		Space:         space,
		Tree:          tree,
		RequestOrigin: resolveRequestOrigin(c),
		Viewer:        viewer,
	}
	if canCreate {
		payload.CreateDocumentURL = h.webOrigin + "/editor/" + url.PathEscape(spaceID) + "?from=reader&create=document"
	}
	h.renderReaderPayload(c, http.StatusOK, spaceID, "", payload, false)
	return true
}

func hasReaderUnsupportedFiles(nodes []service.ReaderTreeNodeViewModel) bool {
	for _, node := range nodes {
		if node.ImportedFileKind == models.ImportedFileKindUnsupported || hasReaderUnsupportedFiles(node.Children) {
			return true
		}
	}
	return false
}

// Page 渲染空间阅读页。
func (h *readerPageHandler) Page(c *gin.Context) {
	if h == nil || h.readerPageService == nil {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(readerFallbackUnavailableHTML))
		return
	}

	spaceID := strings.TrimSpace(c.Param("spaceId"))
	documentID := strings.TrimSpace(c.Param("docId"))
	if spaceID == "" {
		h.renderFriendlyErrorPage(
			c,
			http.StatusBadRequest,
			"链接无效",
			"空间标识不正确，请检查链接后重试。",
		)
		return
	}
	if documentID == "" {
		h.renderFriendlyErrorPage(
			c,
			http.StatusBadRequest,
			"链接无效",
			"文档标识不正确，请检查链接后重试。",
		)
		return
	}

	viewer := h.resolveOptionalViewerIdentity(c)
	viewModel, err := h.readerPageService.BuildPage(
		c.Request.Context(),
		spaceID,
		documentID,
		viewer.UserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
			h.renderFriendlyErrorPage(
				c,
				http.StatusNotFound,
				"页面不存在",
				"文档不存在，或已被删除。",
			)
		case errors.Is(err, service.ErrViewerLoginRequired):
			h.renderReaderAccessDeniedPage(
				c,
				spaceID,
				documentID,
				viewer,
				"当前文档需要登录后访问。",
				true,
			)
		case errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
			h.renderReaderAccessDeniedPage(
				c,
				spaceID,
				documentID,
				viewer,
				"你没有权限访问该文档，请联系空间管理员。",
				false,
			)
		default:
			h.logError("reader page build failed", err, "space_id", spaceID, "document_id", documentID)
			h.renderFriendlyErrorPage(
				c,
				http.StatusInternalServerError,
				"加载失败",
				"阅读页面暂时不可用，请稍后刷新重试。",
			)
		}
		return
	}

	canonicalDocumentRouteKey := strings.TrimSpace(viewModel.Document.RouteKey)
	if canonicalDocumentRouteKey == "" {
		canonicalDocumentRouteKey = strings.TrimSpace(viewModel.Document.ID)
	}
	if canonicalDocumentRouteKey != "" && canonicalDocumentRouteKey != documentID {
		// 兼容历史 node_id/document_id 入参：统一重定向到 canonical 文档路由 key（slug 或 document_id）。
		targetPath := "/r/" + url.PathEscape(spaceID) + "/" + url.PathEscape(canonicalDocumentRouteKey)
		c.Redirect(http.StatusSeeOther, targetPath)
		return
	}
	if _, err := h.authorizeDocumentPassword(c, viewModel.Document.ID, viewer.UserID); err != nil {
		if errors.Is(err, service.ErrDocumentPasswordRequired) {
			manageURL := h.documentPasswordManagementURL(c, spaceID, viewModel.Document.ID, viewer.UserID)
			renderReaderDocumentUnlock(c, http.StatusOK, readerDocumentPath(spaceID, documentID)+"/unlock", manageURL, "")
		} else {
			h.logError("reader document password check failed", err, "document_id", viewModel.Document.ID)
			h.renderFriendlyErrorPage(c, http.StatusInternalServerError, "加载失败", "暂时无法校验文档访问权限，请稍后再试。")
		}
		return
	}

	payload := readerPagePayload{
		CopyDenied:            viewModel.CopyDenied,
		ExportDenied:          viewModel.ExportDenied,
		TrackDocumentActivity: viewer.Authenticated,
		Space:                 viewModel.Space,
		Document:              viewModel.Document,
		Attachments:           viewModel.Attachments,
		Tree:                  viewModel.Tree,
		ActiveDocID:           viewModel.ActiveDocID,
		RequestOrigin:         resolveRequestOrigin(c),
		Viewer:                viewer,
	}
	if viewModel.CanCreateDocument {
		payload.CreateDocumentURL = h.webOrigin + "/editor/" + url.PathEscape(spaceID) + "?from=reader&create=document"
	}
	if viewModel.CanEdit {
		payload.EditURL = h.webOrigin + "/editor/" + url.PathEscape(spaceID) + "/" + url.PathEscape(viewModel.Document.ID) + "?from=reader"
	}
	recordDocumentActivity(c, h.activityRepo, viewer.UserID, viewModel.Document.ID, "view")
	h.renderReaderPayload(c, http.StatusOK, spaceID, documentID, payload, true)
}

// GetOnlyOfficeViewConfig 返回阅读页 Office 文档只读配置。
func (h *readerPageHandler) GetOnlyOfficeViewConfig(c *gin.Context) {
	if h == nil || h.readerPageService == nil || h.onlyOfficeConfigService == nil || h.onlyOfficeTokenService == nil {
		response.InternalError(c)
		return
	}

	spaceID := strings.TrimSpace(c.Param("spaceId"))
	documentID := strings.TrimSpace(c.Param("docId"))
	if spaceID == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidSpaceID, "空间 ID 不能为空")
		return
	}
	if documentID == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidDocumentID, "文档 ID 不能为空")
		return
	}

	appendVaryHeader(c, "Authorization")
	appendVaryHeader(c, "Cookie")
	c.Header("Cache-Control", "private, no-store, max-age=0")

	viewer := h.resolveOptionalViewerIdentity(c)
	viewModel, err := h.readerPageService.BuildPage(
		c.Request.Context(),
		spaceID,
		documentID,
		viewer.UserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
			response.Error(c, http.StatusNotFound, response.CodeDocumentNotFound, "文档不存在，或已被删除")
		case errors.Is(err, service.ErrViewerLoginRequired):
			response.Error(c, http.StatusForbidden, response.CodeUnauthorized, "当前文档需要登录后访问")
		case errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "你没有权限访问该文档")
		default:
			h.logError("reader onlyoffice view config build failed", err, "space_id", spaceID, "document_id", documentID)
			response.InternalError(c)
		}
		return
	}

	lockGeneration, err := h.authorizeDocumentPassword(c, viewModel.Document.ID, viewer.UserID)
	if err != nil {
		writeReaderDocumentPasswordError(c, err)
		return
	}
	configPayload, err := buildOnlyOfficeViewConfig(
		c.Request.Context(),
		h.onlyOfficeConfigService,
		h.onlyOfficeTokenService,
		viewModel.Document,
		!viewModel.CopyDenied,
		!viewModel.ExportDenied,
		viewer.UserID,
		viewer.Name,
		false,
		lockGeneration,
	)
	if err != nil {
		h.logError("reader onlyoffice view config resolve failed", err, "space_id", spaceID, "document_id", documentID)
		switch {
		case errors.Is(err, errOnlyOfficeViewConfigNotOfficeDocument):
			response.Error(c, http.StatusBadRequest, response.CodeInvalidOperation, "当前文档不是 Office 文档")
		case errors.Is(err, errOnlyOfficeViewConfigDisabled):
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "ONLYOFFICE 阅读能力未启用")
		default:
			response.InternalError(c)
		}
		return
	}

	response.JSON(c, http.StatusOK, configPayload)
}

// CreateOfficeSourceAccessLink 返回阅读页 Office 原文件下载链接。
func (h *readerPageHandler) CreateOfficeSourceAccessLink(c *gin.Context) {
	if h == nil || h.readerPageService == nil || h.onlyOfficeTokenService == nil {
		response.InternalError(c)
		return
	}

	spaceID := strings.TrimSpace(c.Param("spaceId"))
	documentID := strings.TrimSpace(c.Param("docId"))
	if spaceID == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidSpaceID, "空间 ID 不能为空")
		return
	}
	if documentID == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidDocumentID, "文档 ID 不能为空")
		return
	}

	appendVaryHeader(c, "Authorization")
	appendVaryHeader(c, "Cookie")
	c.Header("Cache-Control", "private, no-store, max-age=0")

	viewer := h.resolveOptionalViewerIdentity(c)
	viewModel, err := h.readerPageService.BuildPage(
		c.Request.Context(),
		spaceID,
		documentID,
		viewer.UserID,
	)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrSpaceNotFound), errors.Is(err, service.ErrDocumentNotFound):
			response.Error(c, http.StatusNotFound, response.CodeDocumentNotFound, "文档不存在，或已被删除")
		case errors.Is(err, service.ErrViewerLoginRequired):
			response.Error(c, http.StatusForbidden, response.CodeUnauthorized, "当前文档需要登录后访问")
		case errors.Is(err, service.ErrSpaceAccessDenied), errors.Is(err, service.ErrDocumentAccessDenied):
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "你没有权限访问该文档")
		default:
			h.logError("reader office source access link build failed", err, "space_id", spaceID, "document_id", documentID)
			response.InternalError(c)
		}
		return
	}

	if viewModel.ExportDenied {
		writeDocumentExportForbidden(c)
		return
	}
	lockGeneration, err := h.authorizeDocumentPassword(c, viewModel.Document.ID, viewer.UserID)
	if err != nil {
		writeReaderDocumentPasswordError(c, err)
		return
	}
	downloadURL, fileName, expiresAt, err := buildOfficeSourceAccessLink(
		h.onlyOfficeTokenService,
		viewModel.Document,
		viewer.UserID,
		false,
		lockGeneration,
	)
	if err != nil {
		h.logError("reader office source access link resolve failed", err, "space_id", spaceID, "document_id", documentID)
		switch {
		case errors.Is(err, errOnlyOfficeViewConfigNotOfficeDocument):
			response.Error(c, http.StatusBadRequest, response.CodeInvalidOperation, "当前文档不是 Office 文档")
		case errors.Is(err, errOnlyOfficeViewConfigSourceBlobEmpty):
			response.Error(c, http.StatusNotFound, response.CodeDocumentNotFound, "原文件不存在，无法下载")
		default:
			response.InternalError(c)
		}
		return
	}

	response.JSON(c, http.StatusOK, officeSourceAccessLinkResponse{
		URL:       downloadURL,
		FileName:  fileName,
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	})
}

func (h *readerPageHandler) renderReaderPayload(
	c *gin.Context,
	statusCode int,
	spaceID string,
	documentID string,
	payload readerPagePayload,
	cacheable bool,
) {
	if payload.Share == nil || !payload.Share.Enabled {
		payload.SearchEnabled = h.searchConfig.IsEnabled(c.Request.Context())
	}
	if payload.Access == nil && models.IsOfficeDocumentFormat(payload.Document.Format) {
		h.prepareOfficeReaderPayload(c.Request.Context(), &payload)
	}
	applyReaderBrowserCacheHeaders(c)

	if h.dispatcher != nil {
		renderPayload := payload
		if cacheable {
			// 鉴权在上游已完成，渲染缓存不携带用户态，避免缓存碎片与用户信息污染。
			renderPayload.Viewer = readerPageViewerIdentity{}
		}
		payloadBytes, marshalErr := json.Marshal(renderPayload)
		if marshalErr != nil {
			h.logError("marshal reader page payload failed", marshalErr, "space_id", spaceID, "document_id", documentID)
		} else {
			if cacheable && h.renderCache != nil {
				cacheKey := buildReaderRenderCacheKey(h.renderCache, documentID, payloadBytes)
				if cacheKey != "" {
					cachedHTML, cacheErr := h.renderCache.GetOrRender(
						c.Request.Context(),
						strings.TrimSpace(documentID),
						cacheKey,
						func(ctx context.Context) ([]byte, error) {
							return h.renderReaderPayloadByDispatcher(ctx, payloadBytes)
						},
					)
					if cacheErr == nil && len(cachedHTML) > 0 {
						c.Data(statusCode, "text/html; charset=utf-8", cachedHTML)
						return
					}
					if cacheErr != nil {
						h.logWarn(
							"reader page render cache bypassed",
							"space_id",
							spaceID,
							"document_id",
							documentID,
							"error",
							cacheErr.Error(),
						)
					}
				}
			}

			renderedHTML, renderResponse, renderErr := h.renderReaderPayloadByDispatcherWithState(
				c.Request.Context(),
				payloadBytes,
			)
			if renderErr == nil && len(renderedHTML) > 0 {
				c.Data(statusCode, "text/html; charset=utf-8", renderedHTML)
				return
			}
			if renderErr != nil {
				h.logError(
					"reader page ssr render failed",
					renderErr,
					"space_id",
					spaceID,
					"document_id",
					documentID,
				)
			} else if renderResponse != nil {
				h.logWarn(
					"reader page ssr render rejected",
					"space_id",
					spaceID,
					"document_id",
					documentID,
					"error_code",
					resolveReaderErrorCode(renderResponse.Error),
					"error_message",
					resolveReaderErrorMessage(renderResponse.Error),
				)
			}
		}
	}

	c.Data(statusCode, "text/html; charset=utf-8", []byte(buildReaderFallbackHTML(payload)))
}

func applyReaderBrowserCacheHeaders(c *gin.Context) {
	appendVaryHeader(c, "Authorization")
	appendVaryHeader(c, "Cookie")
	// Only reuse rendered HTML inside the server. Every browser request must recheck
	// the document's current password, including requests for previously public pages.
	c.Header("Cache-Control", "private, no-store, max-age=0")
}

func (h *readerPageHandler) renderReaderPayloadByDispatcher(
	ctx context.Context,
	payloadBytes []byte,
) ([]byte, error) {
	renderedHTML, _, renderErr := h.renderReaderPayloadByDispatcherWithState(ctx, payloadBytes)
	return renderedHTML, renderErr
}

func (h *readerPageHandler) renderReaderPayloadByDispatcherWithState(
	ctx context.Context,
	payloadBytes []byte,
) ([]byte, *protocol.RenderResponse, error) {
	if h == nil || h.dispatcher == nil {
		return nil, nil, errors.New("reader page dispatcher not initialized")
	}
	renderResponse, renderErr := h.dispatcher.Render(ctx, protocol.RenderRequest{
		ID:      strings.ToLower(ulid.Make().String()),
		Type:    protocol.MessageTypeRender,
		Route:   "space-reader",
		Payload: payloadBytes,
	})
	if renderErr != nil {
		return nil, nil, renderErr
	}
	if !renderResponse.OK || strings.TrimSpace(renderResponse.HTML) == "" {
		return nil, &renderResponse, nil
	}
	return []byte(renderResponse.HTML), &renderResponse, nil
}

func buildReaderRenderCacheKey(
	cache *rendercache.Cache,
	documentID string,
	payloadBytes []byte,
) string {
	if cache == nil {
		return ""
	}
	normalizedDocumentID := strings.TrimSpace(documentID)
	if normalizedDocumentID == "" || len(payloadBytes) == 0 {
		return ""
	}
	payloadHash := sha256.Sum256(payloadBytes)
	return cache.BuildKey(
		normalizedDocumentID,
		time.Time{},
		hex.EncodeToString(payloadHash[:]),
	)
}

func (h *readerPageHandler) prepareOfficeReaderPayload(ctx context.Context, payload *readerPagePayload) {
	payload.OfficeRendering = h.resolveOfficeRenderingState(ctx)
	onlyOfficeConfig, configErr := h.onlyOfficeConfigService.GetConfig(ctx)
	if configErr == nil && onlyOfficeConfig.Enabled && onlyOfficeConfig.DocumentServerURL != "" {
		return
	}

	payload.EditURL = ""
	payload.OfficeRendering.IndependentRenderEnabled = true
	payload.OfficeRendering.FallbackToOnlyOfficeOnRenderFailure = false
	preview, err := h.officeReaderPreview.Read(ctx, payload.Space.ID, payload.Document)
	if err != nil {
		h.logWarn("local office preview failed", "document_id", payload.Document.ID, "error", err.Error())
		payload.Document.ContentMD = ""
		payload.Document.RenderStatus = models.DocumentRenderStatusFailed
		payload.Document.RenderError = "暂时无法预览此 Office 文件。请稍后重试。"
		return
	}
	payload.Document.ContentMD = preview
	payload.Document.RenderStatus = models.DocumentRenderStatusSuccess
	payload.Document.RenderError = ""
	payload.localOfficeFallback = true
}

func (h *readerPageHandler) resolveOfficeRenderingState(ctx context.Context) readerPageOfficeRenderingState {
	if h == nil || h.officeRenderingConfigService == nil {
		return readerPageOfficeRenderingState{
			IndependentRenderEnabled:            false,
			FallbackToOnlyOfficeOnRenderFailure: true,
		}
	}
	config, err := h.officeRenderingConfigService.GetConfig(ctx)
	if err != nil {
		return readerPageOfficeRenderingState{
			IndependentRenderEnabled:            false,
			FallbackToOnlyOfficeOnRenderFailure: true,
		}
	}
	return readerPageOfficeRenderingState{
		IndependentRenderEnabled:            config.IndependentRenderEnabled,
		FallbackToOnlyOfficeOnRenderFailure: config.FallbackToOnlyOfficeOnRenderFailure,
	}
}

func (h *readerPageHandler) renderReaderAccessDeniedPage(
	c *gin.Context,
	spaceID string,
	documentID string,
	viewer readerPageViewerIdentity,
	description string,
	requiresLogin bool,
) {
	spaceTitle := "空间阅读"
	normalizedSpaceID := strings.TrimSpace(spaceID)
	normalizedDocumentID := strings.TrimSpace(documentID)
	if normalizedDocumentID == "" {
		normalizedDocumentID = "unknown"
	}
	accessDescription := strings.TrimSpace(description)
	if accessDescription == "" {
		accessDescription = "你没有权限访问该文档，请联系空间管理员。"
	}

	spaceViewModel := service.ReaderSpaceViewModel{
		ID:    normalizedSpaceID,
		Name:  spaceTitle,
		Title: "无权限访问 - " + spaceTitle,
	}
	tree := []service.ReaderTreeNodeViewModel{}
	activeDocID := normalizedDocumentID

	// 优先复用同空间下的空间上下文（空间名称与目录树），避免无权限页出现异常标题/空目录。
	if h != nil && h.readerPageService != nil {
		if resolvedSpace, resolvedTree, contextErr := h.readerPageService.BuildSpaceContext(
			c.Request.Context(),
			normalizedSpaceID,
			strings.TrimSpace(viewer.UserID),
		); contextErr == nil {
			if resolvedName := strings.TrimSpace(resolvedSpace.Name); resolvedName != "" {
				spaceTitle = resolvedName
			}
			spaceViewModel = service.ReaderSpaceViewModel{
				ID:    normalizedSpaceID,
				Name:  spaceTitle,
				Title: "无权限访问 - " + spaceTitle,
			}
			tree = resolvedTree
			if activeDocID == "" {
				activeDocID = normalizedDocumentID
			}
		}
	}

	payload := readerPagePayload{
		Space: spaceViewModel,
		Document: service.ReaderDocumentViewModel{
			ID:             normalizedDocumentID,
			NodeID:         normalizedDocumentID,
			ThemeID:        "default",
			Format:         models.DocumentFormatMarkdown,
			Visibility:     models.VisibilityMember,
			Title:          "无权限访问",
			ContentMD:      "",
			Version:        0,
			ContentVersion: 0,
			AuthorNickname: "未知作者",
			UpdatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
		},
		Attachments:   []service.ReaderDocumentAttachmentViewModel{},
		Tree:          tree,
		ActiveDocID:   activeDocID,
		RequestOrigin: resolveRequestOrigin(c),
		Viewer:        viewer,
		Access: &readerPageAccessState{
			Code:          response.CodeForbidden,
			Title:         "无权限访问",
			Description:   accessDescription,
			RequiresLogin: requiresLogin,
		},
	}
	h.renderReaderPayload(
		c,
		http.StatusForbidden,
		normalizedSpaceID,
		normalizedDocumentID,
		payload,
		false,
	)
}

func (h *readerPageHandler) resolveOptionalViewerIdentity(c *gin.Context) readerPageViewerIdentity {
	if h == nil || h.authService == nil {
		return readerPageViewerIdentity{}
	}

	rawToken, ok := optionalAccessTokenFromRequest(c)
	if !ok {
		return readerPageViewerIdentity{}
	}

	session, err := h.authService.Me(c.Request.Context(), rawToken)
	if err != nil {
		return readerPageViewerIdentity{}
	}
	userID := strings.TrimSpace(session.User.ID)
	name := strings.TrimSpace(session.User.Name)
	if name == "" {
		name = strings.TrimSpace(session.User.Email)
	}
	if name == "" {
		name = "用户"
	}
	return readerPageViewerIdentity{
		UserID:        userID,
		Name:          name,
		Authenticated: userID != "",
	}
}

func (h *readerPageHandler) logError(message string, err error, attrs ...any) {
	if h == nil || h.logger == nil {
		return
	}
	baseAttrs := []any{"error", err.Error()}
	h.logger.Error(message, append(baseAttrs, attrs...)...)
}

func (h *readerPageHandler) logWarn(message string, attrs ...any) {
	if h == nil || h.logger == nil {
		return
	}
	h.logger.Warn(message, attrs...)
}

func (h *readerPageHandler) redirectToLogin(c *gin.Context) {
	if c == nil {
		return
	}
	c.Redirect(http.StatusSeeOther, buildWebAuthEntryURL(c, h.webOrigin, "/login"))
}

func resolveRequestOrigin(c *gin.Context) string {
	absoluteURL := strings.TrimSpace(buildRequestAbsoluteURL(c))
	if absoluteURL == "" {
		return ""
	}
	parsedURL, err := url.Parse(absoluteURL)
	if err != nil {
		return ""
	}
	scheme := strings.TrimSpace(parsedURL.Scheme)
	host := strings.TrimSpace(parsedURL.Host)
	if scheme == "" || host == "" {
		return ""
	}
	return scheme + "://" + host
}

func (h *readerPageHandler) renderFriendlyErrorPage(
	c *gin.Context,
	statusCode int,
	title string,
	description string,
) {
	if c == nil {
		return
	}

	appendVaryHeader(c, "Authorization")
	appendVaryHeader(c, "Cookie")
	c.Header("Cache-Control", "private, no-store, max-age=0")
	c.Data(
		statusCode,
		"text/html; charset=utf-8",
		[]byte(buildReaderErrorHTML(statusCode, title, description)),
	)
}

func resolveReaderErrorCode(renderError *protocol.RenderError) string {
	if renderError == nil {
		return ""
	}
	return strings.TrimSpace(renderError.Code)
}

func resolveReaderErrorMessage(renderError *protocol.RenderError) string {
	if renderError == nil {
		return ""
	}
	return strings.TrimSpace(renderError.Message)
}

func buildReaderFallbackHTML(payload readerPagePayload) string {
	pageTitle := strings.TrimSpace(payload.Space.Title)
	if pageTitle == "" {
		pageTitle = config.ProductName + " 阅读页"
	}
	documentTitle := strings.TrimSpace(payload.Document.Title)
	if documentTitle == "" {
		documentTitle = "未命名文档"
	}
	spaceName := strings.TrimSpace(payload.Space.Name)
	if spaceName == "" {
		spaceName = "未命名空间"
	}
	accessTitle := ""
	accessDescription := ""
	isAccessDenied := false
	if payload.Access != nil {
		accessTitle = strings.TrimSpace(payload.Access.Title)
		accessDescription = strings.TrimSpace(payload.Access.Description)
		isAccessDenied = accessTitle != "" || accessDescription != ""
	}

	stateJSONBytes, err := json.Marshal(payload)
	if err != nil {
		stateJSONBytes = []byte("{}")
	}
	stateJSON := escapeJSONForHTMLScript(string(stateJSONBytes))

	escapedPageTitle := template.HTMLEscapeString(composeSEOTitle(pageTitle))
	escapedDocumentTitle := template.HTMLEscapeString(documentTitle)
	escapedSpaceName := template.HTMLEscapeString(spaceName)
	escapedDocument := template.HTMLEscapeString(payload.Document.ContentMD)
	escapedAccessTitle := template.HTMLEscapeString(accessTitle)
	escapedAccessDescription := template.HTMLEscapeString(accessDescription)
	robotsMeta := ""
	isSpaceDirectory := payload.Document.ID == "" && payload.Access == nil
	if models.IsOfficeDocumentFormat(payload.Document.Format) || isSpaceDirectory {
		robotsMeta = `    <meta name="robots" content="noindex, nofollow" />`
	}

	bodyContent := fmt.Sprintf("<pre>%s</pre>", escapedDocument)
	if payload.localOfficeFallback {
		bodyContent = payload.Document.ContentMD
	} else if models.IsOfficeDocumentFormat(payload.Document.Format) && payload.Document.RenderError != "" {
		bodyContent = fmt.Sprintf("<p>%s</p>", template.HTMLEscapeString(payload.Document.RenderError))
	}
	if payload.Document.ImportedFileKind == models.ImportedFileKindImage && payload.Document.ImportedImageURL != "" {
		bodyContent = fmt.Sprintf(`<img class="reader-fallback-image" src="%s" alt="%s" />`,
			template.HTMLEscapeString(payload.Document.ImportedImageURL), escapedDocumentTitle)
	}
	headerContent := fmt.Sprintf(
		`<p class="reader-meta">空间：%s</p><h1 class="reader-title">%s</h1>`,
		escapedSpaceName,
		escapedDocumentTitle,
	)
	if isAccessDenied {
		if accessTitle == "" {
			accessTitle = "无权限访问"
			escapedAccessTitle = template.HTMLEscapeString(accessTitle)
		}
		if accessDescription == "" {
			accessDescription = "你没有权限访问该文档，请联系空间管理员。"
			escapedAccessDescription = template.HTMLEscapeString(accessDescription)
		}
		headerContent = fmt.Sprintf(`<h1 class="reader-title">%s</h1>`, escapedAccessTitle)
		bodyContent = fmt.Sprintf(
			`<section class="reader-fallback-denied"><h2>%s</h2><p>%s</p></section>`,
			escapedAccessTitle,
			escapedAccessDescription,
		)
	}
	if isSpaceDirectory {
		headerContent = fmt.Sprintf(`<h1 class="reader-title">%s</h1>`, escapedSpaceName)
		bodyContent = "<p>此空间暂无可阅读文档。</p>" + renderFallbackSpaceDirectory(payload.Tree)
	} else if hasReaderUnsupportedFiles(payload.Tree) {
		bodyContent += renderFallbackSpaceDirectory(payload.Tree)
	}

	homeNav := ""
	if payload.Share == nil || !payload.Share.Enabled {
		homeNav = `<nav aria-label="空间导航"><a class="reader-home-link" href="/"><span aria-hidden="true">←</span> 返回首页</a>`
		if payload.CreateDocumentURL != "" && !isAccessDenied {
			homeNav += ` <a class="reader-home-link" href="` + template.HTMLEscapeString(payload.CreateDocumentURL) + `" aria-label="新建文档" title="新建文档">+</a>`
		}
		if payload.EditURL != "" && !isAccessDenied {
			homeNav += ` <a class="reader-home-link" href="` + template.HTMLEscapeString(payload.EditURL) + `">编辑文档</a>`
		}
		homeNav += "</nav>"
		if payload.SearchEnabled {
			homeNav += `<form class="reader-search" action="/search" method="get" role="search" aria-label="搜索文档"><input type="search" name="q" aria-label="搜索可阅读的文档" placeholder="搜索文档" required><button type="submit">搜索</button></form>`
		}
	}

	printPolicy := ""
	if payload.ExportDenied {
		printPolicy = `<style>@media print { .reader-shell { display: none !important; } }</style>`
	}

	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
%s
    <title>%s</title>
    %s
    <link rel="preload" href="/assets/google-sans-code-latin-400-normal.woff2" as="font" type="font/woff2" crossorigin />
    <link rel="stylesheet" href="/assets/font-google-sans-code.css?v=20260224" />
    <style>
      body { margin: 0; font-family: "Google Sans Code", "PingFang SC", "Microsoft YaHei", "Helvetica Neue", Arial, sans-serif; background: #f5f7fb; color: #1f2937; }
      .reader-shell { max-width: 980px; margin: 24px auto; background: #fff; border: 1px solid #dbe2ea; border-radius: 14px; padding: 20px; box-shadow: 0 10px 30px rgba(15, 23, 42, 0.06); }
      .reader-meta { font-size: 12px; color: #6b7280; margin-bottom: 12px; }
      .reader-home-link { display: inline-block; margin-bottom: 10px; padding: 7px 10px; border: 1px solid #cbd5e1; border-radius: 8px; color: #334155; font-size: 14px; font-weight: 600; text-decoration: none; }
      .reader-home-link:hover { background: #eef4fb; }
      .reader-search { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 12px; }
      .reader-search input, .reader-search button { border: 1px solid #cbd5e1; border-radius: 8px; background: #fff; color: #334155; padding: 7px 10px; font: inherit; }
      .reader-search button { cursor: pointer; }
      @media print { .reader-home-link, .reader-search { display: none; } }
      .reader-title { font-size: 28px; font-weight: 700; margin: 0 0 18px; }
      .reader-fallback-notice { margin: 0 0 14px; color: #92400e; background: #fff7ed; border: 1px solid #fed7aa; border-radius: 8px; padding: 10px 12px; }
      .reader-fallback-denied { margin: 0; border: 1px solid #e2e8f0; background: #f8fafc; border-radius: 12px; padding: 16px; }
      .reader-fallback-denied h2 { margin: 0; color: #0f172a; font-size: 18px; line-height: 1.4; }
      .reader-fallback-denied p { margin: 8px 0 0; color: #475569; font-size: 14px; line-height: 1.7; }
      .reader-fallback-image { display: block; max-width: 100%%; height: auto; margin: 0 auto; }
      pre { white-space: pre-wrap; word-break: break-word; margin: 0; font-size: 14px; line-height: 1.7; font-family: "Google Sans Code", "SFMono-Regular", Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace; }
    </style>
  </head>
  <body>
    <main class="reader-shell">
      %s
      %s
      <p class="reader-fallback-notice">SSR 渲染暂时不可用，已降级为基础阅读模式。</p>
      %s
    </main>
    <script id="plaindoc-reader-initial-state" type="application/json">%s</script>
    <script src="/assets/document-copy.js" defer></script>
  </body>
</html>`, robotsMeta, escapedPageTitle, printPolicy, homeNav, headerContent, bodyContent, stateJSON)
}

func renderFallbackSpaceDirectory(nodes []service.ReaderTreeNodeViewModel) string {
	var items strings.Builder
	for _, node := range nodes {
		title := template.HTMLEscapeString(node.Title)
		if node.ImportedFileKind == models.ImportedFileKindUnsupported {
			items.WriteString(`<li><details><summary>` + title + `</summary><p>文件无法打开</p>`)
			if node.ImportedOriginalDownloadURL != "" {
				attributes := ""
				if strings.HasPrefix(node.ImportedOriginalDownloadURL, "/api/") {
					attributes = " download"
				}
				items.WriteString(`<a href="` + template.HTMLEscapeString(node.ImportedOriginalDownloadURL) + `"` + attributes + `>下载原文件</a>`)
			}
			items.WriteString(`</details></li>`)
			continue
		}
		children := renderFallbackSpaceDirectory(node.Children)
		if node.Type == models.NodeTypeFolder || children != "" {
			items.WriteString("<li>")
			items.WriteString(title)
			items.WriteString(children)
			items.WriteString("</li>")
		}
	}
	if items.Len() == 0 {
		return ""
	}
	return "<ul>" + items.String() + "</ul>"
}

func escapeJSONForHTMLScript(rawJSON string) string {
	return strings.NewReplacer(
		"<", "\\u003c",
		">", "\\u003e",
		"&", "\\u0026",
	).Replace(rawJSON)
}

func buildReaderErrorHTML(statusCode int, title string, description string) string {
	pageTitle := strings.TrimSpace(title)
	if pageTitle == "" {
		pageTitle = "页面异常"
	}
	pageDescription := strings.TrimSpace(description)
	if pageDescription == "" {
		pageDescription = "阅读页面暂时不可用，请稍后重试。"
	}

	escapedPageTitle := template.HTMLEscapeString(composeSEOTitle(pageTitle))
	escapedErrorTitle := template.HTMLEscapeString(pageTitle)
	escapedDescription := template.HTMLEscapeString(pageDescription)
	escapedStatusCode := template.HTMLEscapeString(fmt.Sprintf("%d", statusCode))

	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>%s</title>
    <link rel="preload" href="/assets/google-sans-code-latin-400-normal.woff2" as="font" type="font/woff2" crossorigin />
    <link rel="stylesheet" href="/assets/font-google-sans-code.css?v=20260224" />
    <style>
      body { margin: 0; min-height: 100vh; display: flex; align-items: center; justify-content: center; padding: 24px; box-sizing: border-box; font-family: "Google Sans Code", "PingFang SC", "Microsoft YaHei", "Helvetica Neue", Arial, sans-serif; color: #1f2937; background: linear-gradient(180deg, #f8fafc 0%%, #eef2ff 100%%); }
      .error-shell { width: 100%%; max-width: 560px; background: #fff; border: 1px solid #dbe2ea; border-radius: 16px; box-shadow: 0 16px 36px rgba(15, 23, 42, 0.08); padding: 24px; }
      .error-code { margin: 0; font-size: 12px; color: #64748b; letter-spacing: 0.04em; text-transform: uppercase; }
      .error-title { margin: 10px 0 8px; font-size: 28px; line-height: 1.25; color: #0f172a; }
      .error-desc { margin: 0; color: #475569; font-size: 14px; line-height: 1.7; }
      .error-actions { margin-top: 18px; display: flex; gap: 10px; flex-wrap: wrap; }
      .error-link { display: inline-flex; align-items: center; justify-content: center; min-height: 36px; padding: 0 14px; border-radius: 10px; border: 1px solid #cbd5e1; color: #0f172a; text-decoration: none; font-size: 13px; font-weight: 600; background: #fff; }
      .error-link:hover { background: #f8fafc; }
      .error-link-primary { border-color: #2563eb; background: #2563eb; color: #fff; }
      .error-link-primary:hover { background: #1d4ed8; }
    </style>
  </head>
  <body>
    <main class="error-shell">
      <p class="error-code">HTTP %s</p>
      <h1 class="error-title">%s</h1>
      <p class="error-desc">%s</p>
      <div class="error-actions">
        <a class="error-link error-link-primary" href="/">返回首页</a>
        <a class="error-link" href="javascript:history.back()">返回上一页</a>
      </div>
    </main>
  </body>
</html>`, escapedPageTitle, escapedStatusCode, escapedErrorTitle, escapedDescription)
}

const readerFallbackUnavailableHTML = `<!doctype html>
<html lang="zh-CN">
  <head>
    <meta charset="utf-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1" />
    <title>` + config.ProductName + ` 阅读页 - ` + seoTitleSuffix + `</title>
  </head>
  <body>
    <p>reader page is temporarily unavailable</p>
  </body>
</html>`

func composeSEOTitle(pageTitle string) string {
	normalizedTitle := strings.TrimSpace(pageTitle)
	if normalizedTitle == "" {
		return seoTitleSuffix
	}
	return normalizedTitle + " - " + seoTitleSuffix
}
