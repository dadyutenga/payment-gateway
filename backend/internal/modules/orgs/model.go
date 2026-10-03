package orgs

import (
	"errors"
	"strings"
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
	ID           string `json:"id"`
	Name         string `json:"name"`
	Slug         string `json:"slug"`
	KYCStatus    string `json:"kyc_status"`
	BusinessName string `json:"business_name,omitempty"`
	TIN          string `json:"tin,omitempty"`
	// AccountKind is merchant|creator, set at signup and immutable after
	// (changing kind is support-assisted, not self-service).
	AccountKind string `json:"account_kind"`
	// Creator profile (support-me page). Empty for merchant accounts.
	DisplayName string `json:"display_name,omitempty"`
	Handle      string `json:"handle,omitempty"`
	Bio         string `json:"bio,omitempty"`
	// LiveMaxTxnAmount / LiveDailyVolumeCap are per-org live guardrail
	// overrides (positive decimals). Empty means platform default.
	LiveMaxTxnAmount   string `json:"live_max_txn_amount,omitempty"`
	LiveDailyVolumeCap string `json:"live_daily_volume_cap,omitempty"`
	// Extended profile (Settings > General / Branding). All optional.
	Address      string `json:"address,omitempty"`
	Phone        string `json:"phone,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
	LogoURL      string `json:"logo_url,omitempty"`
	PrimaryColor string `json:"primary_color,omitempty"`
	// Suspended orgs cannot move live money until unsuspended.
	Suspended       bool      `json:"suspended"`
	SuspendedReason string    `json:"suspended_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// OrgProfileUpdate carries the editable Settings fields. Empty TIN /
// business name handling matches the old behavior (NULL-able columns).
// Creator fields (DisplayName/Handle/Bio) only apply to creator accounts.
type OrgProfileUpdate struct {
	Name         string
	BusinessName string
	TIN          string
	Address      string
	Phone        string
	ContactEmail string
	LogoURL      string
	PrimaryColor string
	DisplayName  string
	Handle       string
	Bio          string
}

// AccountKind values. Merchant is the business track (Phase 1); creator is
// the individual track (lighter individual KYC, single-member default).
const (
	AccountKindMerchant = "merchant"
	AccountKindCreator  = "creator"
)

// ExternalAccountKind maps storage terminology to the terminology exposed to
// individual-track clients. The database value remains "creator" for
// compatibility; only the external display term is renamed.
func ExternalAccountKind(kind string) string {
	switch strings.TrimSpace(kind) {
	case AccountKindCreator:
		return "individual"
	case AccountKindMerchant:
		return "merchant"
	default:
		return strings.TrimSpace(kind)
	}
}

// ExternalAccountKindLabel is the human-readable counterpart used in
// individual-facing messages and labels.
func ExternalAccountKindLabel(kind string) string {
	switch ExternalAccountKind(kind) {
	case "individual":
		return "Individual"
	case "merchant":
		return "Merchant"
	default:
		return "Account"
	}
}

// ParseAccountKind validates a client-supplied kind string. Empty means
// merchant (back-compat for existing signup callers).
func ParseAccountKind(value string) (string, error) {
	switch value {
	case "", AccountKindMerchant:
		return AccountKindMerchant, nil
	case AccountKindCreator:
		return AccountKindCreator, nil
	default:
		return "", errors.New("account_kind must be merchant or creator")
	}
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

// KYCSubmission is one org's verification file. Rewritten on resubmit
// (review fields cleared); organizations.kyc_status stays the enforced
// source of truth. Admin review (approve/reject) lands in a later block.
type KYCSubmission struct {
	OrgID        string `json:"org_id"`
	BusinessName string `json:"business_name"`
	TIN          string `json:"tin"`
	// Creator (individual) KYC. Empty for merchant accounts.
	FullName string `json:"full_name,omitempty"`
	IDType   string `json:"id_type,omitempty"`
	IDNumber string `json:"id_number,omitempty"`
	// Dob is the creator's date of birth (YYYY-MM-DD). 18+ is enforced
	// at submit; under-18 submissions are refused as validation errors
	// so nothing age-identifying is ever stored for them.
	Dob           string `json:"dob,omitempty"`
	IDDocumentURL string `json:"id_document_url"`
	// IDDocumentBackURL is the optional back side of the ID document.
	IDDocumentBackURL string `json:"id_document_back_url,omitempty"`
	// SelfieURL is the v1 selfie photo (simple capture; true liveness
	// detection is future scope, not a blocker).
	SelfieURL       string     `json:"selfie_url,omitempty"`
	SubmittedAt     time.Time  `json:"submitted_at"`
	ReviewedBy      string     `json:"reviewed_by,omitempty"`
	ReviewedAt      *time.Time `json:"reviewed_at,omitempty"`
	RejectionReason string     `json:"rejection_reason,omitempty"`
}

var (
	ErrOrgNotFound                   = errors.New("organization not found")
	ErrNotOrgMember                  = errors.New("not a member of this organization")
	ErrForbidden                     = errors.New("insufficient privileges for this action")
	ErrAlreadyMember                 = errors.New("user is already a member of this organization")
	ErrUserNotFound                  = errors.New("no account found for that email")
	ErrLastOwner                     = errors.New("organization must keep at least one owner")
	ErrOrgNotEmpty                   = errors.New("organization still has apps — delete or move them first")
	ErrInviteNotFound                = errors.New("no pending invite for this user")
	ErrCannotRemoveSelf              = errors.New("use leave instead of removing yourself")
	ErrSingleOrg                     = errors.New("each account belongs to a single organization")
	ErrKYCNotSubmitted               = errors.New("no kyc submission for this organization")
	ErrKYCNotInReview                = errors.New("organization has no submission awaiting review")
	ErrKYCQueueStatusUnknown         = errors.New("unknown kyc queue status")
	ErrReverificationRequired        = errors.New("business name and TIN are locked after verification — resubmit verification to change them")
	ErrReverificationRequiredCreator = errors.New("name and ID are locked after verification — resubmit verification to change them")
	ErrAccountKindImmutable          = errors.New("account kind cannot be changed after signup")
	ErrHandleTaken                   = errors.New("that handle is already taken")
	ErrHandleInvalid                 = errors.New("handle must be 3-30 lowercase letters, numbers, dots, hyphens or underscores")
	ErrSurveyNotFound                = errors.New("no onboarding survey for this organization")
	ErrKindSwitchSubmitted           = errors.New("account kind can only be switched before verification is submitted")
	ErrHandleReserved                = errors.New("that handle is reserved — pick another")
	ErrSupportPageDisabled           = errors.New("this creator is not accepting support yet")
	ErrNotCreatorOrg                 = errors.New("support pages are for creator accounts only")
	ErrNotMerchantOrg                = errors.New("team management is for business accounts only")
	ErrKindSwitchDisabled            = errors.New("account kind cannot be switched — business and creator accounts are fully separate")
)

// KYCAttempt is one immutable history row: every submit and every admin
// decision appends one. The live kyc_submissions row stays the enforced
// source of truth.
type KYCAttempt struct {
	ID                string     `json:"id"`
	OrgID             string     `json:"org_id"`
	BusinessName      string     `json:"business_name"`
	TIN               string     `json:"tin"`
	FullName          string     `json:"full_name,omitempty"`
	IDType            string     `json:"id_type,omitempty"`
	IDNumber          string     `json:"id_number,omitempty"`
	Dob               string     `json:"dob,omitempty"`
	IDDocumentURL     string     `json:"id_document_url"`
	IDDocumentBackURL string     `json:"id_document_back_url,omitempty"`
	SelfieURL         string     `json:"selfie_url,omitempty"`
	Status            string     `json:"status"`
	RejectionReason   string     `json:"rejection_reason,omitempty"`
	ReviewedBy        string     `json:"reviewed_by,omitempty"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// NotificationPrefs gates per-org event notifications. Absent row == all
// enabled. Delivery itself is still the log-only mailer/SMS stub until a
// real provider is wired.
type NotificationPrefs struct {
	OrgID             string `json:"org_id"`
	PaymentUpdated    bool   `json:"payment_updated"`
	PaymentRefunded   bool   `json:"payment_refunded"`
	PaymentExpired    bool   `json:"payment_expired"`
	WithdrawalUpdates bool   `json:"withdrawal_updates"`
	KYCDecisions      bool   `json:"kyc_decisions"`
}

// KYCQueueItem is one row of the admin review queue: the org plus its
// current submission evidence plus the owner's contact (first active
// owner) so reviewers can reach the business.
type KYCQueueItem struct {
	OrgID        string `json:"org_id"`
	OrgName      string `json:"org_name"`
	Slug         string `json:"slug"`
	KYCStatus    string `json:"kyc_status"`
	AccountKind  string `json:"account_kind"`
	BusinessName string `json:"business_name"`
	TIN          string `json:"tin"`
	FullName     string `json:"full_name,omitempty"`
	IDType       string `json:"id_type,omitempty"`
	IDNumber     string `json:"id_number,omitempty"`
	// Dob is shown to reviewers so the 18+ gate can be audited.
	Dob string `json:"dob,omitempty"`
	// Category is the creator's self-reported survey category (empty for
	// merchants) so reviewers see which track each file belongs to.
	Category string `json:"category,omitempty"`
	// Creator survey risk signal (segmentation only — never raises limits).
	SuggestedRiskTier  string    `json:"suggested_risk_tier,omitempty"`
	ExpectedVolumeBand string    `json:"expected_volume_band,omitempty"`
	ExpectedTxnBand    string    `json:"expected_txn_band,omitempty"`
	HasDocument        bool      `json:"has_document"`
	HasBackDocument    bool      `json:"has_back_document"`
	HasSelfie          bool      `json:"has_selfie"`
	SubmittedAt        time.Time `json:"submitted_at"`
	RejectionReason    string    `json:"rejection_reason,omitempty"`
	OwnerEmail         string    `json:"owner_email,omitempty"`
	OwnerName          string    `json:"owner_name,omitempty"`
	OwnerPhone         string    `json:"owner_phone,omitempty"`
}

// ReservedHandle reports whether a handle is blocked from registration:
// platform, auth, payment, and provider-adjacent names that could be used
// to impersonate LipaGO, admins, or money rails.
func ReservedHandle(handle string) bool {
	switch NormalizeHandle(handle) {
	case "admin", "administrator", "lipago", "lipa", "support", "help",
		"api", "app", "c", "www", "blog", "status", "payments", "payment",
		"checkout", "pay", "kyc", "auth", "login", "signin", "register",
		"signup", "settings", "dashboard", "merchant", "merchants",
		"creator", "creators", "static", "assets", "webhooks", "otp",
		"sonicpesa", "mpesa", "m-pesa", "tigo", "tigopesa", "airtel",
		"airtelmoney", "halotel", "halopesa", "vodacom", "m-pawa", "nida",
		"bot", "bank", "crdb", "nmb", "nbc", "exim", "stanbic", "citibank":
		return true
	default:
		return false
	}
}

// SupportLink is one labeled amount button on a creator's public page
// (fixed preset or buyer-entered open amount). The Block 3 payment_links
// shape to converge on once that table lands.
type SupportLink struct {
	ID         string    `json:"id"`
	OrgID      string    `json:"org_id"`
	Label      string    `json:"label"`
	AmountMode string    `json:"amount_mode"`
	Amount     string    `json:"amount,omitempty"`
	SortOrder  int       `json:"sort_order"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
}

// SupportSettings configures a creator's public page: receiving app,
// buyer-entered amount bounds, and the (future, default-off) supporters
// wall toggle.
type SupportSettings struct {
	OrgID              string        `json:"org_id"`
	SupportAppID       string        `json:"support_app_id,omitempty"`
	MinAmount          string        `json:"min_amount,omitempty"`
	MaxAmount          string        `json:"max_amount,omitempty"`
	ShowSupportersWall bool          `json:"show_supporters_wall"`
	Links              []SupportLink `json:"links"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

// SupportLinkInput carries one button definition from the creator.
type SupportLinkInput struct {
	Label      string
	AmountMode string
	Amount     string
	Active     bool
}

// SupportSettingsInput carries the creator-editable page configuration.
type SupportSettingsInput struct {
	SupportAppID string
	MinAmount    string
	MaxAmount    string
	Links        []SupportLinkInput
}

// Creator support page bounds (Part 4): buyer-entered amounts are floored
// at 500 TZS against dust, capped at order time by the live
// per-transaction tier. At most 6 buttons per page.
const (
	CreatorSupportMinAmount = "500"
	maxSupportLinks         = 6
)

// defaultSupportLinks seeds a newly enabled page: one open "custom
// amount" button plus one fixed "Buy me coffee" preset.
func defaultSupportLinks() []SupportLinkInput {
	return []SupportLinkInput{
		{Label: "Custom amount", AmountMode: "open", Active: true},
		{Label: "Buy me coffee", AmountMode: "fixed", Amount: "5000", Active: true},
	}
}

// CreatorOrgInput carries signup fields for a creator account.
type CreatorOrgInput struct {
	DisplayName string
	Handle      string
	Bio         string
}

// Creator onboarding survey vocabulary (Part 2). Values are snake_case
// wire codes; labels live in the frontend.
const (
	CreatorCategoryContentCreator       = "content_creator"
	CreatorCategoryMusicianArtist       = "musician_artist"
	CreatorCategoryFreelancerConsultant = "freelancer_consultant"
	CreatorCategoryCoachEducator        = "coach_educator"
	CreatorCategoryNonprofitCause       = "nonprofit_cause"
	CreatorCategoryPersonalUse          = "personal_use"
	CreatorCategoryOther                = "other"
)

const (
	CreatorReferralSocialMedia   = "social_media"
	CreatorReferralFriend        = "friend_colleague"
	CreatorReferralSearch        = "search_engine"
	CreatorReferralEvent         = "event_conference"
	CreatorReferralAdvertisement = "advertisement"
	CreatorReferralOther         = "other"
)

const (
	CreatorUseSupportTips     = "support_tips"
	CreatorUseDigitalProducts = "digital_products"
	CreatorUseFreelanceWork   = "freelance_work"
	CreatorUseAPIIntegration  = "api_integration"
)

const (
	CreatorVolumeUnder100K = "under_100k"
	CreatorVolume100KTo1M  = "100k_1m"
	CreatorVolume1MTo10M   = "1m_10m"
	CreatorVolume10MTo100M = "10m_100m"
	CreatorVolumeOver100M  = "over_100m"
)

const (
	CreatorTxnUnder50    = "under_50"
	CreatorTxn50To200    = "50_200"
	CreatorTxn200To1000  = "200_1000"
	CreatorTxn1000To5000 = "1000_5000"
	CreatorTxnOver5000   = "over_5000"
)

// Creator risk tiers suggested from self-reported bands. Segmentation
// only: a "high" suggestion flags closer admin attention, it never
// unlocks higher live limits (those still gate on kyc_status).
const (
	CreatorRiskStandard = "standard"
	CreatorRiskElevated = "elevated"
	CreatorRiskHigh     = "high"
)

// CreatorSurvey is one creator org's onboarding answers plus the derived
// starting risk tier.
type CreatorSurvey struct {
	OrgID              string   `json:"org_id"`
	Category           string   `json:"category"`
	CategoryOther      string   `json:"category_other,omitempty"`
	ReferralSource     string   `json:"referral_source"`
	UseCases           []string `json:"use_cases"`
	ExpectedVolumeBand string   `json:"expected_volume_band"`
	ExpectedTxnBand    string   `json:"expected_txn_band"`
	// SuggestedRiskTier is derived from the bands, not stored.
	SuggestedRiskTier string    `json:"suggested_risk_tier"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// CreatorSurveyInput carries survey answers. DisplayName (Q1) is applied
// to the org row alongside the survey upsert so the form submits once.
type CreatorSurveyInput struct {
	DisplayName        string
	Category           string
	CategoryOther      string
	ReferralSource     string
	UseCases           []string
	ExpectedVolumeBand string
	ExpectedTxnBand    string
}

// CreatorKYCInput carries individual verification evidence.
type CreatorKYCInput struct {
	FullName string
	IDType   string
	IDNumber string
	// Dob is the date of birth (YYYY-MM-DD). Must be 18+.
	Dob    string
	DocURL string
	// DocBackURL is the optional back side of the ID document.
	DocBackURL string
	// SelfieURL is the v1 selfie photo (required).
	SelfieURL string
}
