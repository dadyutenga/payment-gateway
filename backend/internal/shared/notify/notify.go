// Package notify defines the minimal interface other modules (contact,
// payments) depend on to raise admin alert-feed events, without importing
// the notifications module directly.
package notify

import "context"

type Writer interface {
	CreateAdminNotification(ctx context.Context, notifType, title, body string, metadata map[string]any) error
}

// Event is the single contract used by business modules to publish a user
// notification. The notifications module owns persistence, templates,
// preferences, idempotency, and delivery fan-out; callers only describe the
// domain event that actually happened.
type Event struct {
	EventType        string
	OrgID            string
	UserID           string
	AdminID          string
	AllAdmins        bool
	ReferenceID      string
	DedupeKey        string
	LinkURL          string
	Data             map[string]any
	Source           string
	CreatedByAdminID string
}

type Dispatcher interface {
	Dispatch(ctx context.Context, event Event) error
}

// NotificationGate lets a caller check whether an admin has muted a given
// notification event type (via the settings module) before writing it.
type NotificationGate interface {
	ShouldNotify(ctx context.Context, eventType string) (bool, error)
}
