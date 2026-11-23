package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
	"github.com/oklog/ulid/v2"
	"gorm.io/gorm"
)

var (
	ErrInvalidCollaboratorRole = errors.New("请选择有效的成员角色")
	ErrInvalidDocumentAccess   = errors.New("请选择有效的文档权限")
	ErrWorkspaceUserNotFound   = errors.New("找不到使用此邮箱的可用账号")
	ErrWorkspaceMemberRequired = errors.New("请先邀请该用户加入空间")
	ErrWorkspaceOwnerProtected = errors.New("无法更改空间所有者的权限")
)

type WorkspaceDocumentAccess string

const (
	WorkspaceAccessInherit    WorkspaceDocumentAccess = "inherit"
	WorkspaceAccessRead       WorkspaceDocumentAccess = "read"
	WorkspaceAccessEdit       WorkspaceDocumentAccess = "edit"
	WorkspaceAccessDenied     WorkspaceDocumentAccess = "denied"
	WorkspaceAccessReadNoCopy WorkspaceDocumentAccess = "read_no_copy"
	WorkspaceAccessEditNoCopy WorkspaceDocumentAccess = "edit_no_copy"
)

type WorkspaceMember struct {
	UserID       string                  `json:"userId"`
	Name         string                  `json:"name"`
	Email        string                  `json:"email"`
	Role         models.Role             `json:"role"`
	Access       WorkspaceDocumentAccess `json:"access,omitempty"`
	ExportDenied bool                    `json:"exportDenied,omitempty"`
}

type WorkspaceCollaborationService struct {
	workspace  repository.WorkspaceRepository
	spaces     repository.SpaceRepository
	users      repository.UserRepository
	visibility *VisibilityService
}

func NewWorkspaceCollaborationService(
	workspace repository.WorkspaceRepository, spaces repository.SpaceRepository,
	users repository.UserRepository, visibility *VisibilityService,
) *WorkspaceCollaborationService {
	return &WorkspaceCollaborationService{workspace: workspace, spaces: spaces, users: users, visibility: visibility}
}

const exampleDocumentTitle = "示例文档"

const exampleDocumentContent = `# 欢迎使用 PlainDoc

这是空间的第一篇文档。你可以直接修改这篇示例，也可以在左侧点击 **新建文档**，在同一个空间继续记录。

## 项目笔记

- 在这里记录想法、会议纪要和待办事项。
- 可以使用 **粗体**、列表和链接整理内容。
- 编辑后的内容会自动保存。
`

func (s *WorkspaceCollaborationService) CreateSpace(
	ctx context.Context, actorID, name string,
) (*models.Space, string, error) {
	category, err := s.workspace.GetDefaultCategory(ctx)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, "", err
	}
	categoryID, categoryName := models.DefaultSpaceCategoryID, models.DefaultSpaceCategoryName
	if category != nil {
		if category.CategoryID != "" {
			categoryID = category.CategoryID
		}
		if category.Name != "" {
			categoryName = category.Name
		}
	}

	now := time.Now().UTC()
	space := &models.Space{
		SpaceID: strings.ToLower(ulid.Make().String()), Name: name,
		CategoryID: categoryID, Category: categoryName, OwnerUserID: actorID,
		Visibility: models.VisibilityMember, Status: models.EntityStatusActive,
		CreatedAt: now, UpdatedAt: now,
	}
	documentID := strings.ToLower(ulid.Make().String())
	node := &models.Node{
		NodeID: documentID, SpaceID: space.SpaceID, Type: models.NodeTypeDoc,
		Title: exampleDocumentTitle, CreatedByUserID: &actorID, UpdatedByUserID: &actorID,
		CreatedAt: now, UpdatedAt: now,
	}
	document := &models.Document{
		DocumentID: documentID, NodeID: documentID, ThemeID: "default",
		Title: exampleDocumentTitle, Format: models.DocumentFormatMarkdown,
		Visibility: space.Visibility, Status: models.EntityStatusActive,
		ContentMD: exampleDocumentContent, Version: 1, ContentVersion: 1,
		CreatedByUserID: &actorID, UpdatedByUserID: &actorID, CreatedAt: now, UpdatedAt: now,
	}
	revision := &models.DocumentRevision{
		DocumentRevisionID: strings.ToLower(ulid.Make().String()), DocumentID: documentID,
		Version: 1, ContentMD: exampleDocumentContent, EditorUserID: &actorID,
		Source: models.RevisionSourceRemote, CreatedAt: now,
	}
	if err := s.workspace.CreateSpaceWithDocument(ctx, space, repository.WorkspaceCreateNodeParams{
		Node: node, Document: document, Revision: revision, TouchSpace: space.SpaceID, TouchedAt: now,
	}); err != nil {
		return nil, "", err
	}
	return space, documentID, nil
}

func (s *WorkspaceCollaborationService) ownedSpace(ctx context.Context, spaceID, actorID string) (*models.Space, error) {
	space, err := s.spaces.GetBySpaceID(ctx, spaceID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSpaceNotFound
	}
	if err != nil {
		return nil, err
	}
	if space.OwnerUserID != actorID || EnsureEntityActive(space.Status, space.BannedAt, space.DeletedAt) != nil {
		return nil, ErrSpaceAccessDenied
	}
	return space, nil
}

type WorkspaceParticipantSpace struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	CanManage  bool              `json:"canManage"`
	CanEdit    bool              `json:"canEdit"`
	Visibility models.Visibility `json:"visibility"`
}

func (s *WorkspaceCollaborationService) ListSpaces(ctx context.Context, actorID string) ([]WorkspaceParticipantSpace, error) {
	spaces, err := s.spaces.ListByUserID(ctx, actorID)
	if err != nil {
		return nil, err
	}
	items := make([]WorkspaceParticipantSpace, 0, len(spaces))
	for _, space := range spaces {
		if EnsureEntityActive(space.Status, space.BannedAt, space.DeletedAt) != nil {
			continue
		}
		canManage := space.OwnerUserID == actorID
		canEdit := canManage
		if !canEdit {
			canEdit, err = s.spaces.HasWriterAccess(ctx, space.SpaceID, actorID)
			if err != nil {
				return nil, err
			}
			if !canEdit {
				permissions, err := s.visibility.DocumentPermissionsByViewer(ctx, space.SpaceID, actorID)
				if err != nil {
					return nil, err
				}
				for _, permission := range permissions {
					if !permission.AccessDenied && permission.Role != models.RoleReader {
						canEdit = true
						break
					}
				}
			}
		}
		items = append(items, WorkspaceParticipantSpace{
			ID: space.SpaceID, Name: space.Name, CanManage: canManage,
			CanEdit: canEdit, Visibility: normalizeVisibility(space.Visibility),
		})
	}
	return items, nil
}

func (s *WorkspaceCollaborationService) ListMembers(
	ctx context.Context, spaceID, actorID, documentID string,
) ([]WorkspaceMember, error) {
	if _, err := s.ownedSpace(ctx, spaceID, actorID); err != nil {
		return nil, err
	}
	var permissions map[string]models.DocumentPermission
	if documentID != "" {
		document, err := s.visibility.getActiveDocument(ctx, documentID)
		if err != nil {
			return nil, err
		}
		if document.SpaceID != spaceID {
			return nil, ErrDocumentNotFound
		}
		rows, err := s.visibility.permissions.List(ctx, spaceID, documentID, "")
		if err != nil {
			return nil, err
		}
		permissions = make(map[string]models.DocumentPermission, len(rows))
		for _, row := range rows {
			permissions[row.UserID] = row
		}
	}

	rows, err := s.spaces.ListMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	items := make([]WorkspaceMember, 0, len(rows))
	for _, row := range rows {
		if row.UserID == actorID {
			continue
		}
		item := WorkspaceMember{UserID: row.UserID, Name: row.Name, Email: row.Email, Role: row.Role}
		if permissions != nil {
			permission, exists := permissions[row.UserID]
			item.Access = workspaceDocumentAccess(permission, exists)
			item.ExportDenied = permission.ExportDenied
		}
		items = append(items, item)
	}
	return items, nil
}

func workspaceDocumentAccess(permission models.DocumentPermission, exists bool) WorkspaceDocumentAccess {
	switch {
	case !exists || permission.InheritAccess:
		return WorkspaceAccessInherit
	case permission.AccessDenied:
		return WorkspaceAccessDenied
	case permission.CopyDenied && permission.Role == models.RoleReader:
		return WorkspaceAccessReadNoCopy
	case permission.CopyDenied:
		return WorkspaceAccessEditNoCopy
	case permission.Role == models.RoleReader:
		return WorkspaceAccessRead
	default:
		return WorkspaceAccessEdit
	}
}

func (s *WorkspaceCollaborationService) InviteMember(
	ctx context.Context, spaceID, actorID, email string, role models.Role,
) error {
	if _, err := s.ownedSpace(ctx, spaceID, actorID); err != nil {
		return err
	}
	if role != models.RoleReader && role != models.RoleCollaborator {
		return ErrInvalidCollaboratorRole
	}
	user, err := s.users.GetByEmail(ctx, strings.ToLower(strings.TrimSpace(email)))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrWorkspaceUserNotFound
	}
	if err != nil {
		return err
	}
	if EnsureEntityActive(user.Status, user.BannedAt, user.DeletedAt) != nil {
		return ErrWorkspaceUserNotFound
	}
	if user.UserID == actorID {
		return ErrWorkspaceOwnerProtected
	}
	return s.spaces.UpsertMember(ctx, repository.UpsertSpaceMemberParams{SpaceID: spaceID, UserID: user.UserID, Role: role})
}

func (s *WorkspaceCollaborationService) RemoveMember(ctx context.Context, spaceID, actorID, userID string) error {
	if _, err := s.ownedSpace(ctx, spaceID, actorID); err != nil {
		return err
	}
	if userID == actorID {
		return ErrWorkspaceOwnerProtected
	}
	_, err := s.spaces.DeleteMember(ctx, spaceID, userID)
	return err
}

func (s *WorkspaceCollaborationService) RenameSpace(ctx context.Context, spaceID, actorID, name string) error {
	if _, err := s.ownedSpace(ctx, spaceID, actorID); err != nil {
		return err
	}
	_, err := s.spaces.UpdateMetadata(ctx, repository.UpdateSpaceMetadataParams{
		SpaceID: spaceID, Name: &name, UpdatedAt: time.Now().UTC(),
	})
	return err
}

func (s *WorkspaceCollaborationService) SetDocumentAccess(
	ctx context.Context, documentID, actorID, userID string, mode *WorkspaceDocumentAccess, exportDenied *bool,
) error {
	if mode == nil && exportDenied == nil {
		return ErrInvalidDocumentAccess
	}
	document, err := s.visibility.getActiveDocument(ctx, documentID)
	if err != nil {
		return err
	}
	if document.SpaceOwnerUserID != actorID {
		return ErrDocumentAccessDenied
	}
	if userID == actorID {
		return ErrWorkspaceOwnerProtected
	}
	member, err := s.spaces.IsMember(ctx, document.SpaceID, userID)
	if err != nil {
		return err
	}
	if !member {
		return ErrWorkspaceMemberRequired
	}

	permission := models.DocumentPermission{
		DocumentID: documentID, UserID: userID, GrantedByUserID: actorID, Role: models.RoleReader,
		InheritAccess: true,
	}
	if mode != nil {
		permission.InheritAccess = *mode == WorkspaceAccessInherit
		switch *mode {
		case WorkspaceAccessInherit, WorkspaceAccessRead:
		case WorkspaceAccessEdit:
			permission.Role = models.RoleCollaborator
		case WorkspaceAccessReadNoCopy:
			permission.CopyDenied = true
		case WorkspaceAccessEditNoCopy:
			permission.Role = models.RoleCollaborator
			permission.CopyDenied = true
		case WorkspaceAccessDenied:
			permission.AccessDenied = true
		default:
			return ErrInvalidDocumentAccess
		}
	}
	return s.visibility.permissions.Set(ctx, permission, mode != nil, exportDenied)
}
