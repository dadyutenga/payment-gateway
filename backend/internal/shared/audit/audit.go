// Package audit defines the minimal interface other modules (users, sms,
// settings) depend on to record admin actions, without importing the
// activitylog module directly — mirrors internal/shared/notify.
package audit

import "context"

type Writer interface {
	LogAdminAction(ctx context.Context, actorID, actorEmail, action, targetType, targetID string, metadata map[string]any) error
}
