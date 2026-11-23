package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/service"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func (h *workspaceHandler) SetCollaborationService(collaboration *service.WorkspaceCollaborationService) {
	h.collaboration = collaboration
}

func (h *workspaceHandler) ListParticipantSpaces(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	items, err := h.collaboration.ListSpaces(c.Request.Context(), actorID)
	if err != nil {
		writeWorkspaceCollaborationError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, items)
}

func (h *workspaceHandler) ListSpaceMembers(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	items, err := h.collaboration.ListMembers(c.Request.Context(), c.Param("spaceId"), actorID, c.Query("documentId"))
	if err != nil {
		writeWorkspaceCollaborationError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, items)
}

func (h *workspaceHandler) InviteSpaceMember(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	var req struct {
		Email string      `json:"email" binding:"required,email,max=254"`
		Role  models.Role `json:"role" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "请输入有效的邮箱和成员角色")
		return
	}
	err := h.collaboration.InviteMember(c.Request.Context(), c.Param("spaceId"), actorID, req.Email, req.Role)
	writeWorkspaceCollaborationResult(c, err)
}

func (h *workspaceHandler) RemoveSpaceMember(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	err := h.collaboration.RemoveMember(c.Request.Context(), c.Param("spaceId"), actorID, c.Param("userId"))
	writeWorkspaceCollaborationResult(c, err)
}

func (h *workspaceHandler) RenameParticipantSpace(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	var req createWorkspaceSpaceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.WorkspaceErrSpaceNameRequired.Write(c)
		return
	}
	name, valid := normalizeWorkspaceSpaceName(req.Name)
	if !valid {
		response.WorkspaceErrSpaceName.Write(c)
		return
	}
	if err := h.collaboration.RenameSpace(c.Request.Context(), c.Param("spaceId"), actorID, name); err != nil {
		writeWorkspaceCollaborationError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, struct{}{})
}

func (h *workspaceHandler) SetDocumentMemberAccess(c *gin.Context) {
	actorID, ok := h.requireActorUserID(c)
	if !ok {
		return
	}
	var req struct {
		Access       *service.WorkspaceDocumentAccess `json:"access"`
		ExportDenied *bool                            `json:"exportDenied"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		writeWorkspaceCollaborationError(c, service.ErrInvalidDocumentAccess)
		return
	}
	err := h.collaboration.SetDocumentAccess(c.Request.Context(), c.Param("docId"), actorID, c.Param("userId"), req.Access, req.ExportDenied)
	writeWorkspaceCollaborationResult(c, err)
}

func writeWorkspaceCollaborationResult(c *gin.Context, err error) {
	if err != nil {
		writeWorkspaceCollaborationError(c, err)
		return
	}
	response.JSON(c, http.StatusOK, struct{}{})
}

func writeWorkspaceCollaborationError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrSpaceNotFound):
		response.WorkspaceErrSpaceNotFound.Write(c)
	case errors.Is(err, service.ErrSpaceAccessDenied):
		response.WorkspaceErrInsufficientSpacePermission.Write(c)
	case errors.Is(err, service.ErrDocumentNotFound), errors.Is(err, service.ErrDocumentAccessDenied):
		writeWorkspaceDocumentAccessError(c, err)
	case errors.Is(err, service.ErrWorkspaceUserNotFound):
		response.Error(c, http.StatusNotFound, response.CodeUserNotFound, err.Error())
	case errors.Is(err, service.ErrWorkspaceMemberRequired), errors.Is(err, service.ErrWorkspaceOwnerProtected),
		errors.Is(err, service.ErrInvalidCollaboratorRole), errors.Is(err, service.ErrInvalidDocumentAccess):
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	default:
		setRequestErrmsg(c, err, "空间协作操作失败")
		response.InternalError(c)
	}
}
