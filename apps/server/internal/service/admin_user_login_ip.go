package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/pkg/errcode"
	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"gorm.io/gorm"
)

type UpdateAdminUserLoginIPAddressesInput struct {
	ActorUserID string
	RequestID   string
	UserID      string
	IPAddresses []string
}

// UpdateLoginIPAddresses replaces a user's exact login IP allowlist and records the change.
func (s *AdminUserService) UpdateLoginIPAddresses(ctx context.Context, input UpdateAdminUserLoginIPAddressesInput) (result []string, err error) {
	defer func() { err = errcode.MapAdminUserError(err) }()
	actorUserID := strings.TrimSpace(input.ActorUserID)
	if err := s.ensurePlatformAdmin(ctx, actorUserID); err != nil {
		return nil, err
	}
	userID := strings.TrimSpace(input.UserID)
	if userID == "" {
		return nil, errcode.ErrAdminUserInvalidUserID
	}
	addresses, err := normalizeLoginIPAddresses(input.IPAddresses)
	if err != nil {
		return nil, errcode.NewAppError(http.StatusBadRequest, response.CodeInvalidRequest, "请填写最多 64 个有效的 IPv4 或 IPv6 地址")
	}
	user, err := s.userRepo.GetByUserID(ctx, userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errcode.ErrAdminUserNotFound
	}
	if err != nil {
		return nil, err
	}
	if user.Status == models.EntityStatusDeleted || user.DeletedAt != nil {
		return nil, errcode.ErrAdminUserAlreadyDeleted
	}
	if previous, err := decodeLoginIPAddresses(user.LoginIPAddressesJSON); err == nil && slices.Equal(previous, addresses) {
		return addresses, nil
	}
	addressesJSON, err := json.Marshal(addresses)
	if err != nil {
		return nil, err
	}
	// Reuse the serialized allowlist in the audit detail. Store both in the same transaction.
	detailJSON := `{"loginIpAddresses":` + string(addressesJSON) + `}`
	updated, err := s.userRepo.UpdateLoginIPAddresses(ctx, userID, string(addressesJSON), &models.AuditLog{
		ActorUserID: &actorUserID,
		Module:      string(AdminAuditModuleUser),
		Action:      string(AdminAuditActionUpdate),
		TargetType:  "user",
		TargetID:    userID,
		Summary:     "user login IPs updated: " + userID,
		DetailJSON:  detailJSON,
		RequestID:   strings.TrimSpace(input.RequestID),
		CreatedAt:   time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, errcode.ErrAdminUserNotFound
	}
	return addresses, nil
}
