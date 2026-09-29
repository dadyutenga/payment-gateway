package httpserver

import (
	"context"

	"lipago/internal/modules/orgs"
)

// pgAuditWriter adapts the orgs repository audit log to the payments
// audit.Writer interface so every admin mutation lands in app.audit_log.
type pgAuditWriter struct {
	repo *orgs.PostgresRepository
}

func (w pgAuditWriter) LogAdminAction(ctx context.Context, actorID, actorEmail, action, targetType, targetID string, metadata map[string]any) error {
	ip := ""
	if metadata != nil {
		if raw, ok := metadata["ip"].(string); ok {
			ip = raw
		}
	}
	return w.repo.WriteAudit(ctx, actorID, actorEmail, action, targetType, targetID, ip, nil, metadata)
}
