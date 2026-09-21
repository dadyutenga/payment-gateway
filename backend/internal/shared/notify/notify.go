// Package notify defines the minimal interface other modules (contact,
// payments) depend on to raise admin alert-feed events, without importing
// the notifications module directly.
package notify

import "context"

type Writer interface {
	CreateAdminNotification(ctx context.Context, notifType, title, body string, metadata map[string]any) error
}

// NotificationGate lets a caller check whether an admin has muted a given
// notification event type (via the settings module) before writing it.
type NotificationGate interface {
	ShouldNotify(ctx context.Context, eventType string) (bool, error)
}
