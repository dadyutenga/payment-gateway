package support

import "time"

const (
	CategoryPayments    = "payments"
	CategoryWithdrawals = "withdrawals"
	CategoryKYC         = "kyc"
	CategoryTechnical   = "technical"
	CategoryBilling     = "billing"
	CategoryOther       = "other"
)

const (
	StatusOpen       = "open"
	StatusPending    = "pending"
	StatusInProgress = "in_progress"
	StatusResolved   = "resolved"
	StatusClosed     = "closed"
)

const (
	PriorityLow    = "low"
	PriorityNormal = "normal"
	PriorityHigh   = "high"
	PriorityUrgent = "urgent"
)

type Ticket struct {
	ID              string        `json:"id"`
	OrgID           string        `json:"org_id"`
	OrgName         string        `json:"org_name,omitempty"`
	CreatedBy       string        `json:"created_by"`
	CreatedByEmail  string        `json:"created_by_email,omitempty"`
	Subject         string        `json:"subject"`
	Category        string        `json:"category"`
	Priority        string        `json:"priority"`
	Status          string        `json:"status"`
	AssignedAdminID string        `json:"assigned_admin_id,omitempty"`
	AssignedEmail   string        `json:"assigned_email,omitempty"`
	CreatedAt       time.Time     `json:"created_at"`
	UpdatedAt       time.Time     `json:"updated_at"`
	ResolvedAt      *time.Time    `json:"resolved_at,omitempty"`
	SLABreach       bool          `json:"sla_breach"`
	Messages        []Message     `json:"messages,omitempty"`
	LinkedRecord    *LinkedRecord `json:"linked_record,omitempty"`
}

type Message struct {
	ID             string       `json:"id"`
	AuthorType     string       `json:"author_type"`
	AuthorID       string       `json:"author_id"`
	AuthorEmail    string       `json:"author_email,omitempty"`
	Body           string       `json:"body"`
	AttachmentRefs []Attachment `json:"attachments,omitempty"`
	InternalNote   bool         `json:"internal_note,omitempty"`
	CreatedAt      time.Time    `json:"created_at"`
}

type Attachment struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type LinkedRecord struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Status    string    `json:"status"`
	Amount    string    `json:"amount,omitempty"`
	Currency  string    `json:"currency,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateInput struct {
	Subject            string `json:"subject"`
	Category           string `json:"category"`
	Description        string `json:"description"`
	LinkedOrderID      string `json:"linked_order_id"`
	LinkedWithdrawalID string `json:"linked_withdrawal_id"`
}

type ListFilter struct {
	Status   string
	Category string
	Priority string
	Queue    string
	Sort     string
	Limit    int
	Offset   int
}

type AdminUpdate struct {
	AssignedAdminID *string `json:"assigned_admin_id"`
	Priority        *string `json:"priority"`
	Status          *string `json:"status"`
}

type MessageInput struct {
	Body         string `json:"body"`
	InternalNote bool   `json:"internal_note"`
}
