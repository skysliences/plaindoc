package service

import (
	"context"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/repository"
)

// RecordDocumentActivity records an operation whose actor and document have already been authorized.
func RecordDocumentActivity(ctx context.Context, repo repository.AuditLogRepository, actor, documentID, action, requestID string) error {
	if repo == nil || actor == "" {
		return nil
	}
	summary := map[string]string{
		"read": "打开文档",
		"edit": "保存文档编辑",
		"copy": "复制文档内容",
	}[action]
	return repo.Create(ctx, &models.AuditLog{
		ActorUserID: &actor,
		Module:      "document_activity",
		Action:      action,
		TargetType:  "document",
		TargetID:    documentID,
		Summary:     summary,
		DetailJSON:  "{}",
		RequestID:   requestID,
		CreatedAt:   time.Now().UTC(),
	})
}
