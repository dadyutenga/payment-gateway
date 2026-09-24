package orgs

import (
	"errors"
	"time"
)

// Role is an org membership role. The permission matrix lives in Can below
// — exactly one place — so handlers never re-implement role logic.
type Role string

const (
	RoleOwner     Role = "owner"
	RoleFinance   Role = "finance"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

// Permission is a capability checked before org/app-scoped handlers run.
type Permission string

const (
	// PermRead covers every read: balances, reports, ledger, orders,
	// withdrawals list, deliveries, webhook/API-key listings.
	PermRead Permission = "read"
	// PermWithdraw covers creating, approving, and rejecting withdrawals.
	PermWithdraw Permission = "withdraw"
	// PermDevelop covers apps, webhook endpoints, and API keys.
	PermDevelop Permission = "develop"
	// PermManageMembers covers inviting, role changes, and removal.
	PermManageMembers Permission = "manage_members"
	// PermManageOrg covers org settings (name, business details).
	PermManageOrg Permission = "manage_org"
	// PermDeleteOrg covers deleting an (empty) org.
	PermDeleteOrg Permission = "delete_org"
)

// Can reports whether a role grants a permission:
//
//	owner:     everything, including members and org deletion.
//	finance:   reads + withdrawals. No webhooks, keys, or members.
//	developer: reads + apps/webhooks/keys. No withdrawals or members.
//	viewer:    reads only.
func Can(role Role, perm Permission) bool {
	switch role {
	case RoleOwner:
		return true
	case RoleFinance:
		return perm == PermRead || perm == PermWithdraw
	case RoleDeveloper:
		return perm == PermRead || perm == PermDevelop
	case RoleViewer:
		return perm == PermRead
	default:
		return false
	}
}

// ParseRole validates a client-supplied role string.
func ParseRole(value string) (Role, error) {
	switch Role(value) {
	case RoleOwner, RoleFinance, RoleDeveloper, RoleViewer:
		return Role(value), nil
	default:
		return "", errors.New("role must be owner, finance, developer, or viewer")
	}
}

// MemberStatus tracks invite lifecycle: invited rows await accept().
type MemberStatus string

const (
	MemberStatusInvited MemberStatus = "invited"
	MemberStatusActive  MemberStatus = "active"
)

type Organization struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Slug         string    `json:"slug"`
	KYCStatus    string    `json:"kyc_status"`
	BusinessName string    `json:"business_name,omitempty"`
	TIN          string    `json:"tin,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// OrganizationWithRole pairs an org with the caller's role in it — what
// list-my-orgs and the org switcher consume.
type OrganizationWithRole struct {
	Organization
	Role   Role         `json:"role"`
	Status MemberStatus `json:"status"`
}

// OrgMember is one membership row with display fields.
type OrgMember struct {
	OrgID     string       `json:"org_id"`
	UserID    string       `json:"user_id"`
	Email     string       `json:"email"`
	FullName  string       `json:"full_name,omitempty"`
	Phone     string       `json:"phone,omitempty"`
	Role      Role         `json:"role"`
	InvitedBy string       `json:"invited_by,omitempty"`
	Status    MemberStatus `json:"status"`
	CreatedAt time.Time    `json:"created_at"`
}

var (
	ErrOrgNotFound      = errors.New("organization not found")
	ErrNotOrgMember     = errors.New("not a member of this organization")
	ErrForbidden        = errors.New("insufficient privileges for this action")
	ErrAlreadyMember    = errors.New("user is already a member of this organization")
	ErrUserNotFound     = errors.New("no account found for that email")
	ErrLastOwner        = errors.New("organization must keep at least one owner")
	ErrOrgNotEmpty      = errors.New("organization still has apps — delete or move them first")
	ErrInviteNotFound   = errors.New("no pending invite for this user")
	ErrCannotRemoveSelf = errors.New("use leave instead of removing yourself")
)
