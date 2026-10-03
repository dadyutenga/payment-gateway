package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"lipago/internal/shared/notify"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
)

type db interface {
	ExecEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) (pgx.CommandTag, error)
	QueryEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) (*pgx.Rows, error)
	QueryRowEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) *pgx.Row
}

type Mailer interface {
	SendEmail(ctx context.Context, to, subject, body string) error
}

type SMSSender interface {
	SendSMS(ctx context.Context, to, message string) error
}

type Service struct {
	db     db
	log    *slog.Logger
	mailer Mailer
	sms    SMSSender
}

func NewService(database db, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{db: database, log: logger}
}

func (s *Service) SetMailer(m Mailer)            { s.mailer = m }
func (s *Service) SetSMSSender(sender SMSSender) { s.sms = sender }

func (s *Service) CreateAdminNotification(ctx context.Context, notifType, title, body string, metadata map[string]any) error {
	data := map[string]any{"body": body}
	data["title"] = title
	for key, value := range metadata {
		data[key] = value
	}
	referenceID := stringValue(metadata, "reference_id")
	if referenceID == "" {
		referenceID = stringValue(metadata, "payment_order_id")
	}
	return s.Dispatch(ctx, notify.Event{
		EventType:   "admin." + strings.TrimPrefix(notifType, "admin."),
		AllAdmins:   true,
		ReferenceID: referenceID,
		DedupeKey:   stringValue(metadata, "dedupe_key"),
		Data:        data,
		Source:      "system",
	})
}

func (s *Service) Dispatch(ctx context.Context, event notify.Event) error {
	if s == nil || s.db == nil {
		return errors.New("notification service is not configured")
	}
	template := templates[event.EventType]
	if template.Title == "" {
		template = Template{Title: event.EventType, Body: "{body}", Severity: "info"}
	}
	if value, ok := event.Data["title"].(string); ok && strings.TrimSpace(value) != "" {
		template.Title = value
	}
	if value, ok := event.Data["severity"].(string); ok && isSeverity(value) {
		template.Severity = value
	}
	if value, ok := event.Data["icon"].(string); ok {
		template.Icon = strings.TrimSpace(value)
	}
	title := render(template.Title, event.Data)
	body := render(template.Body, event.Data)
	if body == "{body}" || body == "" {
		body = render(stringValue(event.Data, "body"), event.Data)
	}
	if event.LinkURL == "" {
		event.LinkURL = stringValue(event.Data, "link_url")
	}
	if event.Source == "" {
		event.Source = "system"
	}
	if event.DedupeKey == "" && event.ReferenceID != "" {
		event.DedupeKey = event.EventType + ":" + event.ReferenceID
	}

	recipients, err := s.recipients(ctx, event)
	if err != nil {
		return err
	}
	for _, recipient := range recipients {
		critical := isCritical(event.EventType, template.Severity)
		inAppEnabled, err := s.channelEnabled(ctx, event, recipient, ChannelInApp, critical)
		if err != nil {
			return err
		}
		id, inserted, err := s.insertNotification(ctx, event, recipient, title, body, template, inAppEnabled)
		if err != nil {
			return err
		}
		if !inserted {
			continue
		}
		if err := s.enqueueChannels(ctx, event, recipient, id, title, body, template.Severity); err != nil {
			return err
		}
	}
	return nil
}

type recipient struct {
	UserID  string
	AdminID string
	OrgID   string
	Email   string
	Phone   string
}

func (s *Service) recipients(ctx context.Context, event notify.Event) ([]recipient, error) {
	if event.AllAdmins || event.AdminID != "" {
		where := ""
		args := []interface{}{}
		if event.AdminID != "" {
			where = "WHERE id = $1::uuid"
			args = append(args, event.AdminID)
		}
		rows, err := s.db.QueryEx(ctx, "SELECT id::text, email FROM app.admin_users "+where, nil, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []recipient
		for rows.Next() {
			var item recipient
			if err := rows.Scan(&item.AdminID, &item.Email); err != nil {
				return nil, err
			}
			out = append(out, item)
		}
		return out, rows.Err()
	}
	if event.UserID != "" {
		var item recipient
		err := s.db.QueryRowEx(ctx, `SELECT id::text, email, COALESCE(phone, '') FROM app.users WHERE id = $1::uuid`, nil, event.UserID).Scan(&item.UserID, &item.Email, &item.Phone)
		if err != nil {
			return nil, err
		}
		item.OrgID = event.OrgID
		return []recipient{item}, nil
	}
	if event.OrgID == "" {
		return nil, errors.New("notification recipient is missing")
	}
	rows, err := s.db.QueryEx(ctx, `
		SELECT u.id::text, u.email, COALESCE(u.phone, ''), m.org_id::text
		FROM app.org_members m JOIN app.users u ON u.id = m.user_id
		WHERE m.org_id = $1::uuid AND m.status = 'active'`, nil, event.OrgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []recipient
	for rows.Next() {
		var item recipient
		if err := rows.Scan(&item.UserID, &item.Email, &item.Phone, &item.OrgID); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Service) insertNotification(ctx context.Context, event notify.Event, to recipient, title, body string, template Template, inAppEnabled bool) (string, bool, error) {
	var id string
	var org any
	if to.OrgID != "" {
		org = to.OrgID
	}
	var user any
	if to.UserID != "" {
		user = to.UserID
	}
	var admin any
	if to.AdminID != "" {
		admin = to.AdminID
	}
	err := s.db.QueryRowEx(ctx, `
		INSERT INTO app.notifications
		(recipient_org_id, recipient_user_id, recipient_admin_id, event_type, reference_id, dedupe_key, title, body, icon, severity, link_url, source, created_by_admin_id, in_app_enabled)
		VALUES (NULLIF($1, '')::uuid, NULLIF($2, '')::uuid, NULLIF($3, '')::uuid, $4, NULLIF($5, '')::uuid, NULLIF($6, ''), $7, $8, $9, $10, $11, $12, NULLIF($13, '')::uuid, $14)
		ON CONFLICT ((COALESCE(recipient_user_id, recipient_admin_id)), dedupe_key) WHERE dedupe_key IS NOT NULL DO NOTHING
		RETURNING id::text`, nil, stringOrEmpty(org), stringOrEmpty(user), stringOrEmpty(admin), event.EventType, event.ReferenceID, event.DedupeKey, title, body, template.Icon, template.Severity, event.LinkURL, event.Source, event.CreatedByAdminID, inAppEnabled).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

func (s *Service) enqueueChannels(ctx context.Context, event notify.Event, to recipient, notificationID, title, body, severity string) error {
	critical := isCritical(event.EventType, severity)
	channels := []struct{ name, destination string }{{ChannelEmail, to.Email}, {ChannelSMS, to.Phone}}
	for _, channel := range channels {
		if channel.destination == "" {
			continue
		}
		enabled, err := s.channelEnabled(ctx, event, to, channel.name, critical)
		if err != nil {
			return err
		}
		if !enabled {
			continue
		}
		_, err = s.db.ExecEx(ctx, `INSERT INTO app.notification_deliveries (notification_id, channel, destination) VALUES ($1::uuid, $2, $3) ON CONFLICT DO NOTHING`, nil, notificationID, channel.name, channel.destination)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) channelEnabled(ctx context.Context, event notify.Event, to recipient, channel string, critical bool) (bool, error) {
	if critical && channel == ChannelInApp {
		return true, nil
	}
	// User settings override workspace defaults. This makes the same
	// fan-out row usable for both org-wide and per-user preferences.
	if to.UserID != "" {
		var enabled bool
		err := s.db.QueryRowEx(ctx, `SELECT enabled FROM app.notification_preferences WHERE scope_kind = 'user' AND scope_id = $1::uuid AND event_type = $2 AND channel = $3`, nil, to.UserID, event.EventType, channel).Scan(&enabled)
		if err == nil {
			return enabled, nil
		}
		if err != pgx.ErrNoRows {
			return false, err
		}
	}
	scopeKind, scopeID := "org", to.OrgID
	if to.AdminID != "" {
		scopeKind, scopeID = "admin", to.AdminID
	}
	if scopeID == "" {
		return true, nil
	}
	var enabled bool
	err := s.db.QueryRowEx(ctx, `SELECT enabled FROM app.notification_preferences WHERE scope_kind = $1 AND scope_id = $2::uuid AND event_type = $3 AND channel = $4`, nil, scopeKind, scopeID, event.EventType, channel).Scan(&enabled)
	if err == pgx.ErrNoRows {
		return true, nil
	}
	return enabled, err
}

func (s *Service) List(ctx context.Context, userID, adminID string, unreadOnly bool, eventType string, limit, offset int) ([]Notification, int64, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	if offset < 0 {
		offset = 0
	}
	args := []interface{}{}
	where := ""
	if adminID != "" {
		where = "n.recipient_admin_id = $1::uuid AND n.in_app_enabled = TRUE"
		args = append(args, adminID)
	} else {
		where = `n.recipient_user_id = $1::uuid AND n.in_app_enabled = TRUE AND (n.recipient_org_id IS NULL OR EXISTS (SELECT 1 FROM app.org_members m WHERE m.user_id = $1::uuid AND m.status = 'active' AND m.org_id = n.recipient_org_id))`
		args = append(args, userID)
	}
	if unreadOnly {
		where += " AND n.read_at IS NULL"
	}
	if strings.TrimSpace(eventType) != "" {
		args = append(args, eventType)
		where += " AND n.event_type = $" + strconv.Itoa(len(args))
	}
	var total int64
	countArgs := append([]interface{}{}, args...)
	if err := s.db.QueryRowEx(ctx, "SELECT COUNT(*) FROM app.notifications n WHERE "+where, nil, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryEx(ctx, `SELECT n.id::text, n.event_type, n.title, n.body, n.icon, n.severity, n.link_url, n.read_at, n.created_at FROM app.notifications n WHERE `+where+` ORDER BY n.read_at NULLS FIRST, n.created_at DESC LIMIT $`+strconv.Itoa(len(args)-1)+` OFFSET $`+strconv.Itoa(len(args)), nil, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Notification, 0)
	for rows.Next() {
		var item Notification
		if err := rows.Scan(&item.ID, &item.EventType, &item.Title, &item.Body, &item.Icon, &item.Severity, &item.LinkURL, &item.ReadAt, &item.CreatedAt); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Service) MarkRead(ctx context.Context, userID, adminID, id string) error {
	where, args := "recipient_user_id = $1::uuid AND id = $2::uuid", []interface{}{userID, id}
	if adminID != "" {
		where, args = "recipient_admin_id = $1::uuid AND id = $2::uuid", []interface{}{adminID, id}
	}
	tag, err := s.db.ExecEx(ctx, "UPDATE app.notifications SET read_at = COALESCE(read_at, NOW()) WHERE "+where, nil, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (s *Service) MarkAllRead(ctx context.Context, userID, adminID string) (int64, error) {
	where, args := "recipient_user_id = $1::uuid", []interface{}{userID}
	if adminID != "" {
		where, args = "recipient_admin_id = $1::uuid", []interface{}{adminID}
	}
	tag, err := s.db.ExecEx(ctx, "UPDATE app.notifications SET read_at = COALESCE(read_at, NOW()) WHERE "+where+" AND read_at IS NULL", nil, args...)
	if err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}

func (s *Service) UnreadCount(ctx context.Context, userID, adminID string) (int64, error) {
	where, args := "recipient_user_id = $1::uuid AND in_app_enabled = TRUE", []interface{}{userID}
	if adminID != "" {
		where, args = "recipient_admin_id = $1::uuid AND in_app_enabled = TRUE", []interface{}{adminID}
	}
	var count int64
	err := s.db.QueryRowEx(ctx, "SELECT COUNT(*) FROM app.notifications WHERE "+where+" AND read_at IS NULL", nil, args...).Scan(&count)
	return count, err
}

func (s *Service) ProcessDueDeliveries(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryEx(ctx, `
		WITH due AS (
			SELECT d.id
			FROM app.notification_deliveries d
			WHERE ((d.status IN ('pending', 'failed') AND d.next_attempt_at <= NOW())
			   OR (d.status = 'processing' AND d.processing_started_at <= NOW() - INTERVAL '10 minutes'))
			ORDER BY d.created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE app.notification_deliveries d
			SET status = 'processing', attempts = d.attempts + 1, processing_started_at = NOW()
			FROM due
			WHERE d.id = due.id
			RETURNING d.id::text, d.notification_id::text, d.channel, d.destination
		)
		SELECT c.id, c.notification_id, c.channel, c.destination, n.title, n.body
		FROM claimed c JOIN app.notifications n ON n.id = c.notification_id`, nil, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type delivery struct{ id, notificationID, channel, destination, title, body string }
	items := []delivery{}
	for rows.Next() {
		var item delivery
		if err := rows.Scan(&item.id, &item.notificationID, &item.channel, &item.destination, &item.title, &item.body); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	processed := 0
	for _, item := range items {
		var sendErr error
		if item.channel == ChannelEmail && s.mailer != nil {
			sendErr = s.mailer.SendEmail(ctx, item.destination, item.title, item.body)
		} else if item.channel == ChannelSMS && s.sms != nil {
			sendErr = s.sms.SendSMS(ctx, item.destination, item.title+": "+item.body)
		} else {
			sendErr = errors.New("notification channel is not configured")
		}
		if sendErr != nil {
			_, _ = s.db.ExecEx(ctx, `UPDATE app.notification_deliveries SET status = 'failed', processing_started_at = NULL, last_error = $2, next_attempt_at = NOW() + INTERVAL '5 minutes' WHERE id = $1::uuid`, nil, item.id, sendErr.Error())
		} else {
			_, _ = s.db.ExecEx(ctx, `UPDATE app.notification_deliveries SET status = 'sent', processing_started_at = NULL, sent_at = NOW(), last_error = '' WHERE id = $1::uuid`, nil, item.id)
		}
		processed++
	}
	return processed, nil
}

func (s *Service) ListPreferences(ctx context.Context, scopeKind, scopeID string) ([]Preference, error) {
	rows, err := s.db.QueryEx(ctx, `SELECT scope_kind, scope_id::text, event_type, channel, enabled FROM app.notification_preferences WHERE scope_kind = $1 AND scope_id = $2::uuid ORDER BY event_type, channel`, nil, scopeKind, scopeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make([]Preference, 0)
	for rows.Next() {
		var item Preference
		if err := rows.Scan(&item.ScopeKind, &item.ScopeID, &item.EventType, &item.Channel, &item.Enabled); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (s *Service) SavePreference(ctx context.Context, scopeKind, scopeID, eventType, channel string, enabled bool) error {
	if scopeKind != "org" && scopeKind != "user" && scopeKind != "admin" {
		return errors.New("invalid preference scope")
	}
	if channel != ChannelInApp && channel != ChannelEmail && channel != ChannelSMS {
		return errors.New("invalid notification channel")
	}
	if _, ok := templates[eventType]; !ok {
		return errors.New("unknown notification event type")
	}
	if channel == ChannelInApp && isCritical(eventType, templates[eventType].Severity) {
		return errors.New("in-app notifications for critical events cannot be disabled")
	}
	_, err := s.db.ExecEx(ctx, `INSERT INTO app.notification_preferences (scope_kind, scope_id, event_type, channel, enabled) VALUES ($1, $2::uuid, $3, $4, $5) ON CONFLICT (scope_kind, scope_id, event_type, channel) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`, nil, scopeKind, scopeID, eventType, channel, enabled)
	return err
}

// ValidatePreferenceScope prevents a customer from reading or changing a
// different workspace's preferences while still allowing merchant owners to
// manage org-scoped defaults and individual users to manage personal ones.
func (s *Service) ValidatePreferenceScope(ctx context.Context, userID, adminID, scopeKind, scopeID string, write bool) error {
	switch scopeKind {
	case "admin":
		if adminID == "" || adminID != scopeID {
			return errors.New("notification preference scope is not accessible")
		}
		return nil
	case "user":
		if adminID != "" || userID == "" || userID != scopeID {
			return errors.New("notification preference scope is not accessible")
		}
		return nil
	case "org":
		if adminID != "" || userID == "" || scopeID == "" {
			return errors.New("notification preference scope is not accessible")
		}
		var allowed bool
		query := `SELECT EXISTS (SELECT 1 FROM app.org_members WHERE org_id = $1::uuid AND user_id = $2::uuid AND status = 'active'`
		if write {
			query += ` AND role = 'owner'`
		}
		query += `)`
		if err := s.db.QueryRowEx(ctx, query, nil, scopeID, userID).Scan(&allowed); err != nil {
			return err
		}
		if !allowed {
			return errors.New("notification preference scope is not accessible")
		}
		return nil
	default:
		return errors.New("invalid preference scope")
	}
}

func (s *Service) Broadcast(ctx context.Context, adminID string, in BroadcastRequest) (string, int, error) {
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Body) == "" {
		return "", 0, errors.New("title and body are required")
	}
	if !isSeverity(in.Severity) {
		in.Severity = "info"
	}
	if in.Target == nil {
		in.Target = map[string]any{"kind": "all"}
	}
	if err := validateBroadcastTarget(in.Target); err != nil {
		return "", 0, err
	}
	targetJSON, err := json.Marshal(in.Target)
	if err != nil {
		return "", 0, err
	}
	var scheduled any
	if strings.TrimSpace(in.ScheduledFor) != "" {
		when, parseErr := time.Parse(time.RFC3339, strings.TrimSpace(in.ScheduledFor))
		if parseErr != nil {
			return "", 0, errors.New("scheduled_for must be an RFC3339 timestamp")
		}
		scheduled = when.UTC()
	}
	var broadcastID string
	err = s.db.QueryRowEx(ctx, `INSERT INTO app.admin_broadcasts (title, body, icon, severity, target, scheduled_for, created_by_admin_id) VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7::uuid) RETURNING id::text`, nil, in.Title, in.Body, in.Icon, in.Severity, string(targetJSON), scheduled, adminID).Scan(&broadcastID)
	if err != nil {
		return "", 0, err
	}
	// Fan-out is claimed by ProcessDueBroadcasts in the API/worker loop.
	return broadcastID, 0, nil
}

type broadcastJob struct {
	id, title, body, icon, severity, target, adminID string
}

// ProcessDueBroadcasts claims queued broadcasts and fans them out in the
// background. The row lock makes running API and standalone workers together
// safe, just like notification delivery processing.
func (s *Service) ProcessDueBroadcasts(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := s.db.QueryEx(ctx, `
		WITH due AS (
			SELECT id
			FROM app.admin_broadcasts
			WHERE (status = 'queued' AND (scheduled_for IS NULL OR scheduled_for <= NOW()))
			   OR (status = 'processing' AND processing_started_at <= NOW() - INTERVAL '10 minutes')
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE app.admin_broadcasts b
			SET status = 'processing', processing_started_at = NOW()
			FROM due
			WHERE b.id = due.id
			RETURNING b.id::text, b.title, b.body, b.icon, b.severity, b.target::text, b.created_by_admin_id::text
		)
		SELECT id, title, body, icon, severity, target, created_by_admin_id FROM claimed`, nil, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	jobs := make([]broadcastJob, 0)
	for rows.Next() {
		var job broadcastJob
		if err := rows.Scan(&job.id, &job.title, &job.body, &job.icon, &job.severity, &job.target, &job.adminID); err != nil {
			return 0, err
		}
		jobs = append(jobs, job)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		var target map[string]any
		if err := json.Unmarshal([]byte(job.target), &target); err != nil {
			_, _ = s.db.ExecEx(ctx, `UPDATE app.admin_broadcasts SET status = 'failed', last_error = $2, processing_started_at = NULL, processed_at = NOW() WHERE id = $1::uuid`, nil, job.id, err.Error())
			processed++
			continue
		}
		count, fanoutErr := s.fanOutBroadcast(ctx, job.id, job.adminID, BroadcastRequest{Title: job.title, Body: job.body, Icon: job.icon, Severity: job.severity, Target: target})
		if fanoutErr != nil {
			_, _ = s.db.ExecEx(ctx, `UPDATE app.admin_broadcasts SET status = 'failed', last_error = $2, processing_started_at = NULL, processed_at = NOW() WHERE id = $1::uuid`, nil, job.id, fanoutErr.Error())
		} else {
			_, _ = s.db.ExecEx(ctx, `UPDATE app.admin_broadcasts SET status = 'sent', recipient_count = $2, last_error = '', processing_started_at = NULL, processed_at = NOW() WHERE id = $1::uuid`, nil, job.id, count)
		}
		processed++
	}
	return processed, nil
}

func (s *Service) fanOutBroadcast(ctx context.Context, broadcastID, adminID string, in BroadcastRequest) (int, error) {
	kind, _ := in.Target["kind"].(string)
	args := []interface{}{}
	where := "m.status = 'active'"
	switch kind {
	case "merchant", "creator":
		where += " AND o.account_kind = $1"
		args = append(args, kind)
	case "org":
		orgID, _ := in.Target["org_id"].(string)
		if orgID == "" {
			return 0, errors.New("target org_id is required")
		}
		where += " AND o.id = $1::uuid"
		args = append(args, orgID)
	case "kyc_status":
		status, _ := in.Target["status"].(string)
		if status == "" {
			return 0, errors.New("target status is required")
		}
		where += " AND o.kyc_status = $1"
		args = append(args, status)
	case "all", "":
	default:
		return 0, errors.New("unsupported broadcast target")
	}
	rows, err := s.db.QueryEx(ctx, `SELECT DISTINCT ON (u.id) u.id::text, m.org_id::text FROM app.org_members m JOIN app.organizations o ON o.id = m.org_id JOIN app.users u ON u.id = m.user_id WHERE `+where+` ORDER BY u.id, m.org_id`, nil, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var userID, orgID string
		if err := rows.Scan(&userID, &orgID); err != nil {
			return count, err
		}
		if err := s.Dispatch(ctx, notify.Event{EventType: "admin.broadcast", UserID: userID, OrgID: orgID, ReferenceID: broadcastID, DedupeKey: "broadcast:" + broadcastID + ":" + userID, Data: map[string]any{"title": in.Title, "body": in.Body, "severity": in.Severity, "icon": in.Icon}, Source: "admin", CreatedByAdminID: adminID}); err != nil {
			return count, err
		}
		count++
	}
	return count, rows.Err()
}

func validateBroadcastTarget(target map[string]any) error {
	kind, _ := target["kind"].(string)
	switch kind {
	case "all", "merchant", "creator":
		return nil
	case "org":
		if value, _ := target["org_id"].(string); strings.TrimSpace(value) != "" {
			if _, err := uuid.Parse(strings.TrimSpace(value)); err != nil {
				return errors.New("target org_id must be a UUID")
			}
			return nil
		}
		return errors.New("target org_id is required")
	case "kyc_status":
		if value, _ := target["status"].(string); strings.TrimSpace(value) != "" {
			switch strings.ToLower(strings.TrimSpace(value)) {
			case "pending", "submitted", "verified", "rejected":
				return nil
			default:
				return errors.New("target status is invalid")
			}
		}
		return errors.New("target status is required")
	default:
		return errors.New("unsupported broadcast target")
	}
}

func (s *Service) SendToOrg(ctx context.Context, adminID, orgID string, in TargetedRequest) error {
	if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Body) == "" {
		return errors.New("title and body are required")
	}
	if !isSeverity(in.Severity) {
		in.Severity = "info"
	}
	var exists bool
	if err := s.db.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.organizations WHERE id = $1::uuid)`, nil, orgID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return errors.New("organization not found")
	}
	return s.Dispatch(ctx, notify.Event{EventType: "admin.broadcast", OrgID: orgID, ReferenceID: orgID, DedupeKey: "admin-org:" + adminID + ":" + orgID + ":" + strconv.FormatInt(time.Now().UnixNano(), 10), Data: map[string]any{"title": in.Title, "body": in.Body, "severity": in.Severity, "icon": in.Icon}, Source: "admin", CreatedByAdminID: adminID})
}

func render(value string, data map[string]any) string {
	for key, raw := range data {
		value = strings.ReplaceAll(value, "{"+key+"}", fmt.Sprint(raw))
	}
	return value
}
func stringValue(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}
func stringOrEmpty(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func isSeverity(value string) bool {
	return value == "info" || value == "success" || value == "warning" || value == "alert"
}
func isCritical(eventType, severity string) bool {
	return severity == "alert" || strings.HasPrefix(eventType, "kyc.") || strings.HasPrefix(eventType, "withdrawal.") || eventType == "org.suspended" || strings.HasPrefix(eventType, "security.")
}
func relativeJSON(data map[string]any) []byte { raw, _ := json.Marshal(data); return raw }
func safeURL(value string) string {
	if u, err := url.Parse(value); err == nil && u.IsAbs() {
		return u.String()
	}
	return value
}
