package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/lifei6671/plaindoc/apps/server/internal/server/response"
	"github.com/lifei6671/plaindoc/apps/server/internal/storage/models"
)

func TestRouter_AdminDocumentActivityCountsAndScope(t *testing.T) {
	database, serve := setupAuthTestRouter(t)
	t.Cleanup(func() { _ = database.Close() })

	ownerID, _, ownerToken := registerAccessUser(t, serve, "activity-owner@example.com")
	spaceAdminID, _, spaceAdminToken := registerAccessUser(t, serve, "activity-space-admin@example.com")
	platformAdminID, _, platformAdminToken := registerAccessUser(t, serve, "activity-platform-admin@example.com")
	grantAdminRole(t, database, spaceAdminID, "space_admin")
	grantAdminRole(t, database, platformAdminID, "platform_admin")

	const spaceID = "01h1activityspace00000000001"
	const otherSpaceID = "01h1activityspace00000000002"
	const documentID = "01h1activitydocument00000001"
	const otherDocumentID = "01h1activitydocument00000002"
	insertAdminTestSpace(t, database, spaceID, "Activity Space", ownerID, "member")
	insertAdminTestSpace(t, database, otherSpaceID, "Other Activity Space", ownerID, "member")
	insertAdminTestDocument(t, database, spaceID, "01h1activitynode000000000001", documentID, "Activity Document", "member")
	insertAdminTestDocument(t, database, otherSpaceID, "01h1activitynode000000000002", otherDocumentID, "Other Activity Document", "member")
	now := time.Now().UTC()
	if err := database.ORM.Table((models.SpaceAdminScope{}).TableName()).Create(map[string]any{
		models.SpaceAdminScopeColumns.UserID:    spaceAdminID,
		models.SpaceAdminScopeColumns.SpaceID:   spaceID,
		models.SpaceAdminScopeColumns.CreatedAt: now,
		models.SpaceAdminScopeColumns.UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("insert activity space admin scope: %v", err)
	}

	for _, entry := range []struct {
		module     string
		action     string
		targetType string
		targetID   string
		actor      *string
	}{
		{"document_activity", "read", "document", documentID, &ownerID},
		{"document_activity", "read", "document", documentID, &ownerID},
		{"document_activity", "view", "document", documentID, nil},
		{"document_activity", "edit", "document", documentID, &ownerID},
		{"document_activity", "download", "document", documentID, nil},
		{"document_activity", "copy", "document", documentID, &ownerID},
		{"document", "edit", "document", documentID, &platformAdminID},
		{"document_activity", "edit", "space", documentID, &ownerID},
		{"document_activity", "download", "document", "unrelated-document", nil},
	} {
		if err := database.ORM.Create(&models.AuditLog{
			ActorUserID: entry.actor,
			Module:      entry.module,
			Action:      entry.action,
			TargetType:  entry.targetType,
			TargetID:    entry.targetID,
			DetailJSON:  "{}",
			CreatedAt:   now,
		}).Error; err != nil {
			t.Fatalf("insert activity audit log: %v", err)
		}
	}

	for _, tc := range []struct {
		name     string
		token    string
		targetID string
		wantCode int
		counts   [4]int64
	}{
		{name: "scoped counts", token: spaceAdminToken, targetID: documentID, counts: [4]int64{3, 1, 1, 1}},
		{name: "platform counts", token: platformAdminToken, targetID: documentID, counts: [4]int64{3, 1, 1, 1}},
		{name: "zero counts", token: platformAdminToken, targetID: otherDocumentID},
		{name: "unmanaged space", token: spaceAdminToken, targetID: otherDocumentID, wantCode: response.ResolveErrorCode(response.CodeForbidden)},
		{name: "no admin role", token: ownerToken, targetID: documentID, wantCode: response.ResolveErrorCode(response.CodeForbidden)},
		{name: "missing document", token: platformAdminToken, targetID: "missing-document", wantCode: response.ResolveErrorCode(response.CodeDocumentNotFound)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/admin/documents/"+tc.targetID+"/activity", nil)
			req.Header.Set("Authorization", "Bearer "+tc.token)
			rec := serve(req)
			if got := decodeJSONResultCode(t, rec.Body.Bytes()); got != tc.wantCode {
				t.Fatalf("activity business code = %d, want %d, body = %s", got, tc.wantCode, rec.Body.String())
			}
			if tc.wantCode != 0 {
				return
			}
			counts := decodeJSONResultData[map[string]int64](t, rec.Body.Bytes())
			for i, field := range []string{"openCount", "viewCount", "editCount", "downloadCount"} {
				if count, found := counts[field]; !found || count != tc.counts[i] {
					t.Errorf("%s = %d (present %t), want %d", field, count, found, tc.counts[i])
				}
			}
		})
	}
}
