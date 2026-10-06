package support

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"lipago/internal/modules/orgs"
	"lipago/internal/platform/storage"
	"lipago/internal/shared/notify"

	"github.com/google/uuid"
	"github.com/jackc/pgx"
)

type db interface {
	ExecEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) (pgx.CommandTag, error)
	QueryEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) (*pgx.Rows, error)
	QueryRowEx(context.Context, string, *pgx.QueryExOptions, ...interface{}) *pgx.Row
}

type Service struct {
	db         db
	orgs       *orgs.Service
	dispatcher notify.Dispatcher
	storage    storage.Store
	slaAge     time.Duration
	log        *slog.Logger
}

func NewService(database db, orgService *orgs.Service, logger *slog.Logger, slaAge time.Duration) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	if slaAge <= 0 {
		slaAge = 24 * time.Hour
	}
	return &Service{db: database, orgs: orgService, log: logger, slaAge: slaAge}
}

func (s *Service) SetNotificationDispatcher(dispatcher notify.Dispatcher) { s.dispatcher = dispatcher }
func (s *Service) SetStorage(store storage.Store)                         { s.storage = store }

var (
	ErrTicketNotFound     = errors.New("support ticket not found")
	ErrMessageNotFound    = errors.New("support message not found")
	ErrTicketForbidden    = errors.New("support ticket access denied")
	ErrStorageUnavailable = errors.New("file storage is not configured")
)

// ValidationError marks a client-correctable input problem so handlers can
// return 422 without matching on error text.
type ValidationError struct{ Message string }

func (e *ValidationError) Error() string { return e.Message }

func invalid(message string) error { return &ValidationError{Message: message} }

func validCategory(value string) bool {
	switch strings.TrimSpace(value) {
	case CategoryPayments, CategoryWithdrawals, CategoryKYC, CategoryTechnical, CategoryBilling, CategoryOther:
		return true
	default:
		return false
	}
}

func validStatus(value string) bool {
	switch strings.TrimSpace(value) {
	case StatusOpen, StatusPending, StatusInProgress, StatusResolved, StatusClosed:
		return true
	default:
		return false
	}
}

func validPriority(value string) bool {
	switch strings.TrimSpace(value) {
	case PriorityLow, PriorityNormal, PriorityHigh, PriorityUrgent:
		return true
	default:
		return false
	}
}

func (s *Service) authorize(ctx context.Context, userID, orgID string) error {
	if s.orgs == nil || userID == "" || orgID == "" {
		return ErrTicketForbidden
	}
	if _, err := s.orgs.CheckOrgPermission(ctx, userID, orgID, orgs.PermRead); err != nil {
		return ErrTicketForbidden
	}
	return nil
}

func (s *Service) resolveIndividual(ctx context.Context, userID string) (string, error) {
	if s.orgs == nil {
		return "", ErrTicketForbidden
	}
	account, err := s.orgs.IndividualAccountForUser(ctx, userID)
	if err != nil {
		return "", ErrTicketForbidden
	}
	return account.ID, nil
}

func (s *Service) validateLinks(ctx context.Context, orgID string, in CreateInput) error {
	if in.LinkedOrderID != "" && in.LinkedWithdrawalID != "" {
		return invalid("link only one order or withdrawal")
	}
	if in.LinkedOrderID != "" {
		var exists bool
		err := s.db.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.payment_orders p JOIN app.payment_apps a ON a.id = p.app_id WHERE p.id = $1::uuid AND a.org_id = $2::uuid)`, nil, in.LinkedOrderID, orgID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return invalid("linked order was not found in this account")
		}
	}
	if in.LinkedWithdrawalID != "" {
		var exists bool
		err := s.db.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.payment_withdrawals w JOIN app.payment_apps a ON a.id = w.app_id WHERE w.id = $1::uuid AND a.org_id = $2::uuid)`, nil, in.LinkedWithdrawalID, orgID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return invalid("linked withdrawal was not found in this account")
		}
	}
	return nil
}

func (s *Service) CreateTicket(ctx context.Context, userID, orgID string, in CreateInput) (Ticket, error) {
	if err := s.authorize(ctx, userID, orgID); err != nil {
		return Ticket{}, err
	}
	in.Subject = strings.TrimSpace(in.Subject)
	in.Category = strings.TrimSpace(in.Category)
	in.Description = strings.TrimSpace(in.Description)
	if in.Subject == "" || len([]rune(in.Subject)) > 160 || !validCategory(in.Category) || in.Description == "" || len([]rune(in.Description)) > 10000 {
		return Ticket{}, invalid("invalid support ticket details")
	}
	if err := s.validateLinks(ctx, orgID, in); err != nil {
		return Ticket{}, err
	}
	var ticketID string
	err := s.db.QueryRowEx(ctx, `
		WITH created AS (
			INSERT INTO app.support_tickets (org_id, created_by, subject, category, linked_order_id, linked_withdrawal_id)
			VALUES ($1::uuid, $2::uuid, $3, $4, NULLIF($5, '')::uuid, NULLIF($6, '')::uuid)
			RETURNING id
		)
		INSERT INTO app.support_messages (ticket_id, author_type, author_id, body)
		SELECT id, 'merchant', $2::uuid, $7 FROM created
		RETURNING ticket_id::text`, nil, orgID, userID, in.Subject, in.Category, in.LinkedOrderID, in.LinkedWithdrawalID, in.Description).Scan(&ticketID)
	if err != nil {
		return Ticket{}, err
	}
	s.notifyAdmins(ctx, ticketID, orgID, in.Subject)
	return s.GetTicket(ctx, userID, "", ticketID)
}

func (s *Service) notifyAdmins(ctx context.Context, ticketID, orgID, subject string) {
	if s.dispatcher == nil {
		return
	}
	if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: "support.ticket_created", AllAdmins: true, OrgID: orgID, ReferenceID: ticketID, DedupeKey: "support.ticket_created:" + ticketID, LinkURL: "/admin/support/" + ticketID, Data: map[string]any{"subject": subject}, Source: "system"}); err != nil {
		s.log.Error("support ticket admin notification failed", "ticket_id", ticketID, "error", err)
	}
}

func (s *Service) customerTicketLink(ctx context.Context, orgID, ticketID string) string {
	var kind string
	if err := s.db.QueryRowEx(ctx, `SELECT COALESCE(account_kind, 'merchant') FROM app.organizations WHERE id = $1::uuid`, nil, orgID).Scan(&kind); err == nil && kind == orgs.AccountKindCreator {
		return "/individual/support/" + ticketID
	}
	return "/merchant/support/" + ticketID
}

func (s *Service) notifyCustomer(ctx context.Context, eventType, ticketID, orgID, userID, subject, status, dedupeSuffix string) {
	if s.dispatcher == nil || userID == "" {
		return
	}
	if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: eventType, UserID: userID, OrgID: orgID, ReferenceID: ticketID, DedupeKey: eventType + ":" + ticketID + ":" + dedupeSuffix, LinkURL: s.customerTicketLink(ctx, orgID, ticketID), Data: map[string]any{"subject": subject, "status": status}, Source: "system"}); err != nil {
		s.log.Error("support ticket customer notification failed", "ticket_id", ticketID, "error", err)
	}
}

func (s *Service) notifyAssignedOrAdmins(ctx context.Context, eventType, ticketID, orgID, subject, dedupeSuffix string) {
	var adminID string
	if err := s.db.QueryRowEx(ctx, `SELECT COALESCE(assigned_admin_id::text, '') FROM app.support_tickets WHERE id = $1::uuid`, nil, ticketID).Scan(&adminID); err != nil || adminID == "" {
		if s.dispatcher != nil {
			if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: eventType, AllAdmins: true, OrgID: orgID, ReferenceID: ticketID, DedupeKey: eventType + ":" + ticketID + ":" + dedupeSuffix, LinkURL: "/admin/support/" + ticketID, Data: map[string]any{"subject": subject}, Source: "system"}); err != nil {
				s.log.Error("support admin notification failed", "ticket_id", ticketID, "event", eventType, "error", err)
			}
		}
		return
	}
	if s.dispatcher != nil {
		if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: eventType, AdminID: adminID, ReferenceID: ticketID, DedupeKey: eventType + ":" + ticketID + ":" + dedupeSuffix, LinkURL: "/admin/support/" + ticketID, Data: map[string]any{"subject": subject}, Source: "system"}); err != nil {
			s.log.Error("support admin notification failed", "ticket_id", ticketID, "event", eventType, "error", err)
		}
	}
}

func (s *Service) ListTickets(ctx context.Context, userID, adminID, orgID string, filter ListFilter) ([]Ticket, int64, error) {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 25
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	args := []interface{}{}
	where := []string{"1=1"}
	if adminID == "" {
		if err := s.authorize(ctx, userID, orgID); err != nil {
			return nil, 0, err
		}
		args = append(args, orgID)
		where = append(where, "t.org_id = $"+fmt.Sprint(len(args))+"::uuid")
	} else {
		switch filter.Queue {
		case "unassigned":
			where = append(where, "t.assigned_admin_id IS NULL")
		case "assigned_to_me":
			args = append(args, adminID)
			where = append(where, "t.assigned_admin_id = $"+fmt.Sprint(len(args))+"::uuid")
		case "", "all":
		default:
			return nil, 0, invalid("invalid support queue")
		}
	}
	if filter.Status != "" {
		if !validStatus(filter.Status) {
			return nil, 0, invalid("invalid support status")
		}
		args = append(args, filter.Status)
		where = append(where, "t.status = $"+fmt.Sprint(len(args)))
	}
	if filter.Category != "" {
		if !validCategory(filter.Category) {
			return nil, 0, invalid("invalid support category")
		}
		args = append(args, filter.Category)
		where = append(where, "t.category = $"+fmt.Sprint(len(args)))
	}
	if filter.Priority != "" {
		if !validPriority(filter.Priority) {
			return nil, 0, invalid("invalid support priority")
		}
		args = append(args, filter.Priority)
		where = append(where, "t.priority = $"+fmt.Sprint(len(args)))
	}
	whereSQL := strings.Join(where, " AND ")
	var total int64
	if err := s.db.QueryRowEx(ctx, "SELECT COUNT(*) FROM app.support_tickets t WHERE "+whereSQL, nil, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "t.created_at ASC"
	if filter.Sort == "priority" {
		order = "CASE t.priority WHEN 'urgent' THEN 1 WHEN 'high' THEN 2 WHEN 'normal' THEN 3 ELSE 4 END, t.created_at ASC"
	}
	args = append(args, filter.Limit, filter.Offset)
	rows, err := s.db.QueryEx(ctx, `SELECT t.id::text, t.org_id::text, COALESCE(o.name, ''), t.created_by::text, COALESCE(u.email, ''), t.subject, t.category, t.priority, t.status, COALESCE(t.assigned_admin_id::text, ''), COALESCE(a.email, ''), t.created_at, t.updated_at, t.resolved_at FROM app.support_tickets t JOIN app.organizations o ON o.id = t.org_id JOIN app.users u ON u.id = t.created_by LEFT JOIN app.admin_users a ON a.id = t.assigned_admin_id WHERE `+whereSQL+` ORDER BY `+order+` LIMIT $`+fmt.Sprint(len(args)-1)+` OFFSET $`+fmt.Sprint(len(args)), nil, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	items := make([]Ticket, 0)
	for rows.Next() {
		var item Ticket
		if err := rows.Scan(&item.ID, &item.OrgID, &item.OrgName, &item.CreatedBy, &item.CreatedByEmail, &item.Subject, &item.Category, &item.Priority, &item.Status, &item.AssignedAdminID, &item.AssignedEmail, &item.CreatedAt, &item.UpdatedAt, &item.ResolvedAt); err != nil {
			return nil, 0, err
		}
		item.SLABreach = s.isSLABreach(item)
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (s *Service) isSLABreach(ticket Ticket) bool {
	return ticket.Status != StatusResolved && ticket.Status != StatusClosed && time.Since(ticket.CreatedAt) > s.slaAge
}

func (s *Service) GetTicket(ctx context.Context, userID, adminID, ticketID string) (Ticket, error) {
	args := []interface{}{ticketID}
	where := "t.id = $1::uuid"
	if adminID == "" {
		if userID == "" {
			return Ticket{}, ErrTicketForbidden
		}
		args = append(args, userID)
		where += " AND EXISTS (SELECT 1 FROM app.org_members m WHERE m.org_id = t.org_id AND m.user_id = $2::uuid AND m.status = 'active')"
	}
	var item Ticket
	err := s.db.QueryRowEx(ctx, `SELECT t.id::text, t.org_id::text, COALESCE(o.name, ''), t.created_by::text, COALESCE(u.email, ''), t.subject, t.category, t.priority, t.status, COALESCE(t.assigned_admin_id::text, ''), COALESCE(a.email, ''), t.created_at, t.updated_at, t.resolved_at FROM app.support_tickets t JOIN app.organizations o ON o.id = t.org_id JOIN app.users u ON u.id = t.created_by LEFT JOIN app.admin_users a ON a.id = t.assigned_admin_id WHERE `+where, nil, args...).Scan(&item.ID, &item.OrgID, &item.OrgName, &item.CreatedBy, &item.CreatedByEmail, &item.Subject, &item.Category, &item.Priority, &item.Status, &item.AssignedAdminID, &item.AssignedEmail, &item.CreatedAt, &item.UpdatedAt, &item.ResolvedAt)
	if err == pgx.ErrNoRows {
		return Ticket{}, ErrTicketNotFound
	}
	if err != nil {
		return Ticket{}, err
	}
	item.SLABreach = s.isSLABreach(item)
	messageWhere := "m.ticket_id = $1::uuid"
	if adminID == "" {
		messageWhere += " AND m.internal_note = FALSE"
	}
	rows, err := s.db.QueryEx(ctx, `SELECT m.id::text, m.author_type, m.author_id::text, CASE WHEN m.author_type = 'admin' THEN COALESCE(ad.email, '') ELSE COALESCE(u.email, '') END, m.body, m.attachment_refs, m.internal_note, m.created_at FROM app.support_messages m LEFT JOIN app.users u ON m.author_type = 'merchant' AND u.id = m.author_id LEFT JOIN app.admin_users ad ON m.author_type = 'admin' AND ad.id = m.author_id WHERE `+messageWhere+` ORDER BY m.created_at ASC`, nil, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	defer rows.Close()
	item.Messages = make([]Message, 0)
	for rows.Next() {
		var message Message
		var refs []byte
		if err := rows.Scan(&message.ID, &message.AuthorType, &message.AuthorID, &message.AuthorEmail, &message.Body, &refs, &message.InternalNote, &message.CreatedAt); err != nil {
			return Ticket{}, err
		}
		_ = json.Unmarshal(refs, &message.AttachmentRefs)
		item.Messages = append(item.Messages, message)
	}
	if err := rows.Err(); err != nil {
		return Ticket{}, err
	}
	item.LinkedRecord, err = s.linkedRecord(ctx, item.OrgID, ticketID)
	return item, err
}

func (s *Service) linkedRecord(ctx context.Context, orgID, ticketID string) (*LinkedRecord, error) {
	var kind, id, status, amount, currency string
	var created time.Time
	err := s.db.QueryRowEx(ctx, `SELECT 'order', p.id::text, p.status, p.amount::text, p.currency, p.created_at FROM app.support_tickets t JOIN app.payment_orders p ON p.id = t.linked_order_id JOIN app.payment_apps a ON a.id = p.app_id WHERE t.id = $1::uuid AND t.org_id = $2::uuid UNION ALL SELECT 'withdrawal', w.id::text, w.status, w.amount::text, w.currency, w.created_at FROM app.support_tickets t JOIN app.payment_withdrawals w ON w.id = t.linked_withdrawal_id JOIN app.payment_apps a ON a.id = w.app_id WHERE t.id = $1::uuid AND t.org_id = $2::uuid LIMIT 1`, nil, ticketID, orgID).Scan(&kind, &id, &status, &amount, &currency, &created)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &LinkedRecord{ID: id, Kind: kind, Status: status, Amount: amount, Currency: currency, CreatedAt: created}, nil
}

func (s *Service) AddCustomerReply(ctx context.Context, userID, ticketID, body string) (Ticket, error) {
	ticket, err := s.GetTicket(ctx, userID, "", ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.Status == StatusClosed {
		return Ticket{}, invalid("this ticket is closed; please open a new ticket")
	}
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > 10000 {
		return Ticket{}, invalid("message body is required and must be 10000 characters or fewer")
	}
	if _, err := s.db.ExecEx(ctx, `INSERT INTO app.support_messages (ticket_id, author_type, author_id, body) VALUES ($1::uuid, 'merchant', $2::uuid, $3)`, nil, ticketID, userID, body); err != nil {
		return Ticket{}, err
	}
	if _, err := s.db.ExecEx(ctx, `UPDATE app.support_tickets SET status = CASE WHEN status = 'resolved' OR status = 'closed' THEN 'open' ELSE status END, updated_at = NOW(), resolved_at = NULL WHERE id = $1::uuid`, nil, ticketID); err != nil {
		return Ticket{}, err
	}
	s.notifyAssignedOrAdmins(ctx, "support.customer_replied", ticketID, ticket.OrgID, ticket.Subject, time.Now().UTC().Format(time.RFC3339Nano))
	return s.GetTicket(ctx, userID, "", ticketID)
}

func (s *Service) AddAdminReply(ctx context.Context, adminID, ticketID string, in MessageInput) (Ticket, error) {
	ticket, err := s.GetTicket(ctx, "", adminID, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	in.Body = strings.TrimSpace(in.Body)
	if in.Body == "" || len([]rune(in.Body)) > 10000 {
		return Ticket{}, invalid("message body is required and must be 10000 characters or fewer")
	}
	if _, err := s.db.ExecEx(ctx, `INSERT INTO app.support_messages (ticket_id, author_type, author_id, body, internal_note) VALUES ($1::uuid, 'admin', $2::uuid, $3, $4)`, nil, ticketID, adminID, in.Body, in.InternalNote); err != nil {
		return Ticket{}, err
	}
	if _, err := s.db.ExecEx(ctx, `UPDATE app.support_tickets SET updated_at = NOW() WHERE id = $1::uuid`, nil, ticketID); err != nil {
		return Ticket{}, err
	}
	if !in.InternalNote {
		s.notifyCustomer(ctx, "support.admin_replied", ticketID, ticket.OrgID, ticket.CreatedBy, ticket.Subject, ticket.Status, time.Now().UTC().Format(time.RFC3339Nano))
	}
	return s.GetTicket(ctx, "", adminID, ticketID)
}

func (s *Service) CloseByCustomer(ctx context.Context, userID, ticketID string) (Ticket, error) {
	ticket, err := s.GetTicket(ctx, userID, "", ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if ticket.Status != StatusResolved {
		return Ticket{}, invalid("only resolved tickets can be closed")
	}
	if _, err := s.db.ExecEx(ctx, `UPDATE app.support_tickets SET status = 'closed', updated_at = NOW(), resolved_at = COALESCE(resolved_at, NOW()) WHERE id = $1::uuid`, nil, ticketID); err != nil {
		return Ticket{}, err
	}
	s.notifyAssignedOrAdmins(ctx, "support.status_changed", ticketID, ticket.OrgID, ticket.Subject, "status:closed")
	return s.GetTicket(ctx, userID, "", ticketID)
}

func (s *Service) UpdateByAdmin(ctx context.Context, adminID, ticketID string, in AdminUpdate) (Ticket, error) {
	ticket, err := s.GetTicket(ctx, "", adminID, ticketID)
	if err != nil {
		return Ticket{}, err
	}
	if in.Priority != nil && !validPriority(*in.Priority) {
		return Ticket{}, invalid("invalid support priority")
	}
	if in.Status != nil && !validStatus(*in.Status) {
		return Ticket{}, invalid("invalid support status")
	}
	assigned := ticket.AssignedAdminID
	if in.AssignedAdminID != nil {
		assigned = strings.TrimSpace(*in.AssignedAdminID)
		if assigned != "" {
			var exists bool
			if err := s.db.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.admin_users WHERE id = $1::uuid)`, nil, assigned).Scan(&exists); err != nil || !exists {
				return Ticket{}, invalid("assigned admin was not found")
			}
		}
	}
	status := ticket.Status
	if in.Status != nil {
		status = *in.Status
	}
	priority := ticket.Priority
	if in.Priority != nil {
		priority = *in.Priority
	}
	if _, err := s.db.ExecEx(ctx, `UPDATE app.support_tickets SET assigned_admin_id = NULLIF($2, '')::uuid, priority = $3, status = $4, resolved_at = CASE WHEN $4 IN ('resolved', 'closed') THEN COALESCE(resolved_at, NOW()) ELSE NULL END, updated_at = NOW() WHERE id = $1::uuid`, nil, ticketID, assigned, priority, status); err != nil {
		return Ticket{}, err
	}
	if in.Status != nil && status != ticket.Status {
		eventType := "support.status_changed"
		if status == StatusResolved {
			eventType = "support.ticket_resolved"
		}
		s.notifyCustomer(ctx, eventType, ticketID, ticket.OrgID, ticket.CreatedBy, ticket.Subject, status, "status:"+status)
	}
	return s.GetTicket(ctx, "", adminID, ticketID)
}

func (s *Service) AddAttachmentMessage(ctx context.Context, userID, adminID, ticketID, body string, attachment Attachment) (Ticket, error) {
	var ticket Ticket
	var err error
	if adminID != "" {
		ticket, err = s.GetTicket(ctx, "", adminID, ticketID)
	} else {
		ticket, err = s.GetTicket(ctx, userID, "", ticketID)
	}
	if err != nil {
		return Ticket{}, err
	}
	if adminID == "" && ticket.Status == StatusClosed {
		return Ticket{}, invalid("this ticket is closed; please open a new ticket")
	}
	body = strings.TrimSpace(body)
	if body == "" {
		body = "Attachment"
	}
	if len([]rune(body)) > 10000 {
		return Ticket{}, invalid("message body must be 10000 characters or fewer")
	}
	refs, _ := json.Marshal([]Attachment{attachment})
	authorType, authorID := "merchant", userID
	if adminID != "" {
		authorType, authorID = "admin", adminID
	}
	if _, err := s.db.ExecEx(ctx, `INSERT INTO app.support_messages (ticket_id, author_type, author_id, body, attachment_refs, internal_note) VALUES ($1::uuid, $2, $3::uuid, $4, $5::jsonb, FALSE)`, nil, ticketID, authorType, authorID, body, string(refs)); err != nil {
		return Ticket{}, err
	}
	_, _ = s.db.ExecEx(ctx, `UPDATE app.support_tickets SET updated_at = NOW() WHERE id = $1::uuid`, nil, ticketID)
	if adminID != "" {
		s.notifyCustomer(ctx, "support.admin_replied", ticketID, ticket.OrgID, ticket.CreatedBy, ticket.Subject, ticket.Status, time.Now().UTC().Format(time.RFC3339Nano))
	} else {
		s.notifyAssignedOrAdmins(ctx, "support.customer_replied", ticketID, ticket.OrgID, ticket.Subject, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if adminID != "" {
		return s.GetTicket(ctx, "", adminID, ticketID)
	}
	return s.GetTicket(ctx, userID, "", ticketID)
}

func (s *Service) Attachment(ctx context.Context, userID, adminID, ticketID, messageID string) (Attachment, error) {
	var ticket Ticket
	var err error
	if adminID != "" {
		ticket, err = s.GetTicket(ctx, "", adminID, ticketID)
	} else {
		ticket, err = s.GetTicket(ctx, userID, "", ticketID)
	}
	if err != nil {
		return Attachment{}, err
	}
	_ = ticket
	var raw []byte
	err = s.db.QueryRowEx(ctx, `SELECT attachment_refs FROM app.support_messages WHERE id = $1::uuid AND ticket_id = $2::uuid`, nil, messageID, ticketID).Scan(&raw)
	if err == pgx.ErrNoRows {
		return Attachment{}, ErrMessageNotFound
	}
	if err != nil {
		return Attachment{}, err
	}
	var refs []Attachment
	if err := json.Unmarshal(raw, &refs); err != nil || len(refs) == 0 || refs[0].Key == "" {
		return Attachment{}, ErrMessageNotFound
	}
	return refs[0], nil
}

func (s *Service) GetAttachment(ctx context.Context, attachment Attachment) (storage.Object, error) {
	if s.storage == nil {
		return storage.Object{}, ErrStorageUnavailable
	}
	return s.storage.Get(ctx, attachment.Key)
}

func (s *Service) StoreAttachment(ctx context.Context, ticketID, filename, contentType string, size int64, body io.Reader) (Attachment, error) {
	if s.storage == nil {
		return Attachment{}, ErrStorageUnavailable
	}
	if size < 0 || size > 5<<20 {
		return Attachment{}, invalid("attachment exceeds the 5 MB limit")
	}
	ext := strings.ToLower(filepath.Ext(filename))
	allowed := map[string]bool{".pdf": true, ".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".txt": true, ".csv": true}
	if err := storage.ValidateUpload(contentType, filename, size, 5<<20, allowed); err != nil {
		return Attachment{}, invalid(err.Error())
	}
	key, err := storage.BuildKey("private/support", ticketID, uuid.NewString()+ext, allowed)
	if err != nil {
		return Attachment{}, err
	}
	info, err := s.storage.Put(ctx, key, body, contentType, size)
	if err != nil {
		return Attachment{}, err
	}
	return Attachment{Key: info.Key, Name: filepath.Base(filename), ContentType: contentType, Size: size}, nil
}
