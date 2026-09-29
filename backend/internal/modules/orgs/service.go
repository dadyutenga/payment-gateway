package orgs

import (
	"context"
	"log/slog"
	"math/big"
	"strings"

	"lipago/internal/shared/validation"
)

// Service owns organization membership rules. Role enforcement for
// app-scoped handlers lives here too via RoleForApp/Can, so there is one
// decision point no matter which module asks.
type Service struct {
	repo Repository
	log  *slog.Logger
}

func NewService(repo Repository, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{repo: repo, log: logger}
}

// RoleForApp resolves the caller's active role for an app through its org.
// Inactive (invited-only) memberships grant nothing. Unknown users and
// unknown apps surface as not-a-member so callers map everything to 403
// without leaking which half was wrong.
func (s *Service) RoleForApp(ctx context.Context, userID, appID string) (Role, error) {
	orgID, err := s.repo.GetAppOrgID(ctx, strings.TrimSpace(appID))
	if err != nil {
		return "", ErrNotOrgMember
	}
	member, err := s.repo.GetMember(ctx, orgID, userID)
	if err != nil {
		return "", ErrNotOrgMember
	}
	if member.Status != MemberStatusActive {
		return "", ErrNotOrgMember
	}
	return member.Role, nil
}

// CheckAppPermission is the single gate for app-scoped actions.
func (s *Service) CheckAppPermission(ctx context.Context, userID, appID string, perm Permission) (Role, error) {
	role, err := s.RoleForApp(ctx, userID, appID)
	if err != nil {
		return "", err
	}
	if !Can(role, perm) {
		return role, ErrForbidden
	}
	return role, nil
}

// CheckOrgPermission is the single gate for org-scoped actions. It returns
// the caller's active membership for handlers that need it.
func (s *Service) CheckOrgPermission(ctx context.Context, userID, orgID string, perm Permission) (OrgMember, error) {
	member, err := s.repo.GetMember(ctx, strings.TrimSpace(orgID), userID)
	if err != nil {
		return OrgMember{}, ErrNotOrgMember
	}
	if member.Status != MemberStatusActive {
		return OrgMember{}, ErrNotOrgMember
	}
	if !Can(member.Role, perm) {
		return member, ErrForbidden
	}
	return member, nil
}

func (s *Service) CreateOrganization(ctx context.Context, userID, name, businessName string) (Organization, validation.Errors, error) {
	name = strings.TrimSpace(name)
	businessName = strings.TrimSpace(businessName)

	errs := validation.Errors{}
	validation.Required(name, "Name is required.", errs, "name")
	validation.MaxRunes(name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.MaxRunes(businessName, 200, "Business name must be 200 characters or fewer.", errs, "business_name")
	if errs.Any() {
		return Organization{}, errs, nil
	}

	// One org per account.
	if count, err := s.repo.CountActiveOrgsForUser(ctx, userID); err != nil {
		return Organization{}, nil, err
	} else if count > 0 {
		return Organization{}, nil, ErrSingleOrg
	}

	org, err := s.repo.CreateOrganization(ctx, name, SlugFor(name), businessName, userID)
	if err != nil {
		return Organization{}, nil, err
	}
	return org, nil, nil
}

func (s *Service) ListMyOrganizations(ctx context.Context, userID string) ([]OrganizationWithRole, error) {
	return s.repo.ListOrganizationsForUser(ctx, userID)
}

func (s *Service) GetOrganization(ctx context.Context, userID, orgID string) (OrganizationWithRole, error) {
	member, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead)
	if err != nil {
		return OrganizationWithRole{}, err
	}
	org, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return OrganizationWithRole{}, err
	}
	return OrganizationWithRole{Organization: org, Role: member.Role, Status: member.Status}, nil
}

func (s *Service) UpdateOrganization(ctx context.Context, userID, orgID string, upd OrgProfileUpdate) (Organization, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return Organization{}, nil, err
	}
	upd.Name = strings.TrimSpace(upd.Name)
	upd.BusinessName = strings.TrimSpace(upd.BusinessName)
	upd.TIN = strings.TrimSpace(upd.TIN)
	upd.Address = strings.TrimSpace(upd.Address)
	upd.Phone = strings.TrimSpace(upd.Phone)
	upd.ContactEmail = strings.ToLower(strings.TrimSpace(upd.ContactEmail))
	upd.LogoURL = strings.TrimSpace(upd.LogoURL)
	upd.PrimaryColor = strings.TrimSpace(upd.PrimaryColor)

	errs := validation.Errors{}
	validation.Required(upd.Name, "Name is required.", errs, "name")
	validation.MaxRunes(upd.Name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.MaxRunes(upd.BusinessName, 200, "Business name must be 200 characters or fewer.", errs, "business_name")
	validation.MaxRunes(upd.Address, 500, "Address must be 500 characters or fewer.", errs, "address")
	validation.MaxRunes(upd.Phone, 30, "Phone must be 30 characters or fewer.", errs, "phone")
	if upd.ContactEmail != "" && !strings.Contains(upd.ContactEmail, "@") {
		errs.Add("contact_email", "Contact email must be a valid address.")
	}
	validation.MaxRunes(upd.LogoURL, 500, "Logo URL must be 500 characters or fewer.", errs, "logo_url")
	if upd.PrimaryColor != "" && !validHexColor(upd.PrimaryColor) {
		errs.Add("primary_color", "Primary color must be a hex color like #0ea5e9.")
	}
	if errs.Any() {
		return Organization{}, errs, nil
	}

	// Verified orgs lock business name + TIN: changing them requires
	// re-verification (resubmit KYC, which flips status back to submitted).
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return Organization{}, nil, err
	}
	if current.KYCStatus == "verified" &&
		(upd.BusinessName != current.BusinessName || upd.TIN != current.TIN) {
		return Organization{}, nil, ErrReverificationRequired
	}

	org, err := s.repo.UpdateOrganization(ctx, strings.TrimSpace(orgID), upd)
	return org, nil, err
}

// validHexColor accepts #rgb or #rrggbb.
func validHexColor(value string) bool {
	if len(value) != 4 && len(value) != 7 {
		return false
	}
	if value[0] != '#' {
		return false
	}
	for _, r := range value[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return false
		}
	}
	return true
}

func (s *Service) DeleteOrganization(ctx context.Context, userID, orgID string) error {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermDeleteOrg); err != nil {
		return err
	}
	count, err := s.repo.CountApps(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return err
	}
	if count > 0 {
		return ErrOrgNotEmpty
	}
	return s.repo.DeleteOrganization(ctx, strings.TrimSpace(orgID))
}

// AdminListMembers lists members without an actor check (admin tool
// bypass — the RequireAdmin gate replaces the membership check).
func (s *Service) AdminListMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	return s.repo.ListMembers(ctx, strings.TrimSpace(orgID))
}

// ReviewKYC records an admin approve/reject decision. No actor check —
// the RequireAdmin route gate replaces membership (mirrors
// AdminAddMember). Rejections require a reason; approvals ignore it.
func (s *Service) ReviewKYC(ctx context.Context, reviewerEmail, orgID string, approve bool, reason string) (Organization, validation.Errors, error) {
	orgID = strings.TrimSpace(orgID)
	reason = strings.TrimSpace(reason)
	if !approve {
		errs := validation.Errors{}
		validation.Required(reason, "A rejection reason is required.", errs, "reason")
		validation.MaxRunes(reason, 500, "Reason must be 500 characters or fewer.", errs, "reason")
		if errs.Any() {
			return Organization{}, errs, nil
		}
		org, err := s.repo.ReviewKYC(ctx, orgID, "rejected", reviewerEmail, reason)
		return org, nil, err
	}
	org, err := s.repo.ReviewKYC(ctx, orgID, "verified", reviewerEmail, "")
	return org, nil, err
}

// ListKYCQueue returns the admin review queue. Empty status selects the
// actionable submitted queue; verified/rejected give history; all lists
// every submitted file. No actor check — route-gated.
func (s *Service) ListKYCQueue(ctx context.Context, status string) ([]KYCQueueItem, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "", "submitted", "verified", "rejected", "pending", "all":
		if status == "" {
			status = "submitted"
		}
		return s.repo.ListKYCQueue(ctx, status)
	default:
		return nil, ErrKYCQueueStatusUnknown
	}
}

// validPositiveDecimal reports whether value is a positive decimal number
// (empty is allowed — it means "platform default").
func validPositiveDecimal(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	amount, ok := new(big.Rat).SetString(value)
	if !ok || amount.Sign() <= 0 {
		return false
	}
	return true
}

// AdminOrgDetail is the operator's single-org view (linked from
// analytics merchant rows): profile, members, verification evidence and
// history. No actor check — route-gated by RequireAdminAuth.
type AdminOrgDetail struct {
	Org       Organization `json:"org"`
	Members   []OrgMember  `json:"members"`
	KYC       *KYCSubmission `json:"kyc,omitempty"`
	Attempts  []KYCAttempt `json:"attempts"`
}

func (s *Service) AdminOrgDetail(ctx context.Context, orgID string) (AdminOrgDetail, error) {
	orgID = strings.TrimSpace(orgID)
	org, err := s.repo.GetOrganization(ctx, orgID)
	if err != nil {
		return AdminOrgDetail{}, err
	}
	members, err := s.repo.ListMembers(ctx, orgID)
	if err != nil {
		return AdminOrgDetail{}, err
	}
	var kyc *KYCSubmission
	if sub, found, err := s.repo.GetKYCSubmission(ctx, orgID); err != nil {
		return AdminOrgDetail{}, err
	} else if found {
		kyc = &sub
	}
	attempts, err := s.repo.ListKYCAttempts(ctx, orgID)
	if err != nil {
		return AdminOrgDetail{}, err
	}
	return AdminOrgDetail{Org: org, Members: members, KYC: kyc, Attempts: attempts}, nil
}

// AdminGetKYCSubmission loads the evidence row with no actor check —
// the RequireAdmin route gate replaces membership (document review).
func (s *Service) AdminGetKYCSubmission(ctx context.Context, orgID string) (KYCSubmission, error) {
	sub, found, err := s.repo.GetKYCSubmission(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return KYCSubmission{}, err
	}
	if !found {
		return KYCSubmission{}, ErrKYCNotSubmitted
	}
	return sub, nil
}

// UpdateOrgLiveLimits sets per-org live guardrail overrides. Empty clears
// a side back to the platform default. No actor check — route-gated.
func (s *Service) UpdateOrgLiveLimits(ctx context.Context, orgID, maxTxn, dailyCap string) (Organization, validation.Errors, error) {
	maxTxn = strings.TrimSpace(maxTxn)
	dailyCap = strings.TrimSpace(dailyCap)

	errs := validation.Errors{}
	if !validPositiveDecimal(maxTxn) {
		errs.Add("live_max_txn_amount", "Must be a positive number, or empty for the platform default.")
	}
	if !validPositiveDecimal(dailyCap) {
		errs.Add("live_daily_volume_cap", "Must be a positive number, or empty for the platform default.")
	}
	if errs.Any() {
		return Organization{}, errs, nil
	}
	org, err := s.repo.UpdateOrgLiveLimits(ctx, orgID, maxTxn, dailyCap)
	return org, nil, err
}

// ListKYCAttempts returns the submit/decide history for the Settings
// verification tab (any active member may read).
func (s *Service) ListKYCAttempts(ctx context.Context, userID, orgID string) ([]KYCAttempt, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return nil, err
	}
	return s.repo.ListKYCAttempts(ctx, strings.TrimSpace(orgID))
}

// GetNotificationPrefs returns the org's toggles (any active member).
func (s *Service) GetNotificationPrefs(ctx context.Context, userID, orgID string) (NotificationPrefs, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return NotificationPrefs{}, err
	}
	return s.repo.GetNotificationPrefs(ctx, strings.TrimSpace(orgID))
}

// UpdateNotificationPrefs replaces the org's toggles (owner/manage_org).
func (s *Service) UpdateNotificationPrefs(ctx context.Context, userID, orgID string, prefs NotificationPrefs) (NotificationPrefs, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return NotificationPrefs{}, err
	}
	prefs.OrgID = strings.TrimSpace(orgID)
	return s.repo.UpsertNotificationPrefs(ctx, prefs)
}

// PlatformStats returns the admin dashboard snapshot. No actor check —
// route-gated by RequireAdminAuth.
func (s *Service) PlatformStats(ctx context.Context) (PlatformStats, error) {
	return s.repo.PlatformStats(ctx)
}

// SetOrgLogoURL stores the org's logo location (owner/manage_org). The
// handler validates and stores the file; this records the reference.
func (s *Service) SetOrgLogoURL(ctx context.Context, userID, orgID, logoURL string) (Organization, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return Organization{}, err
	}
	return s.repo.UpdateOrgLogo(ctx, strings.TrimSpace(orgID), strings.TrimSpace(logoURL))
}

// SuspendOrg freezes an org's live money movement. No actor check —
// route-gated; the handler audits with the reviewer's identity. Reason
// required (shown to the org).
func (s *Service) SuspendOrg(ctx context.Context, orgID, reason string) (Organization, validation.Errors, error) {
	orgID = strings.TrimSpace(orgID)
	reason = strings.TrimSpace(reason)
	errs := validation.Errors{}
	validation.Required(reason, "A suspension reason is required.", errs, "reason")
	validation.MaxRunes(reason, 500, "Reason must be 500 characters or fewer.", errs, "reason")
	if errs.Any() {
		return Organization{}, errs, nil
	}
	org, err := s.repo.SuspendOrg(ctx, orgID, reason)
	return org, nil, err
}

// UnsuspendOrg lifts a suspension (route-gated, audited by the handler).
func (s *Service) UnsuspendOrg(ctx context.Context, orgID string) (Organization, error) {
	return s.repo.UnsuspendOrg(ctx, strings.TrimSpace(orgID))
}

// OrgSuspensionStatus resolves an app to its org's suspension state with
// no actor check — money-movement gates calling it are already authorized
// via key or membership.
func (s *Service) OrgSuspensionStatus(ctx context.Context, appID string) (bool, string, error) {
	return s.repo.OrgSuspensionByApp(ctx, strings.TrimSpace(appID))
}

// WriteAudit records an admin mutation (best-effort by convention).
func (s *Service) WriteAudit(ctx context.Context, actorID, actorEmail, action, targetType, targetID, ip string, before, after map[string]any) error {
	return s.repo.WriteAudit(ctx, actorID, actorEmail, action, targetType, targetID, ip, before, after)
}

// ListAudit returns the admin trail newest-first (route-gated).
func (s *Service) ListAudit(ctx context.Context, action, actor string, limit, offset int) ([]AuditEntry, error) {
	return s.repo.ListAudit(ctx, action, actor, limit, offset)
}

// OrgKYCStatus returns an app's org KYC status with no actor check —
// callers (order/key gates) are already authorized via key or membership.
func (s *Service) OrgKYCStatus(ctx context.Context, appID string) (string, error) {
	orgID, err := s.repo.GetAppOrgID(ctx, strings.TrimSpace(appID))
	if err != nil {
		return "", err
	}
	return s.KYCStatusForOrg(ctx, orgID)
}

// KYCStatusForOrg returns an org's KYC status with no actor check — same
// contract as OrgKYCStatus, for callers holding the org id directly (app
// creation's initial-key decision). Unknown orgs error.
func (s *Service) KYCStatusForOrg(ctx context.Context, orgID string) (string, error) {
	org, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return "", err
	}
	return org.KYCStatus, nil
}

func validTIN(tin string) bool {
	digits := 0
	for _, r := range tin {
		if r >= '0' && r <= '9' {
			digits++
		} else if r != ' ' && r != '-' {
			return false
		}
	}
	return digits >= 9 && digits <= 20
}

// SubmitKYC files (or refiles) verification evidence for owner-run orgs.
// Business name, TIN, and an uploaded document are all required; review
// (approve/reject) is an admin action in a later block.
func (s *Service) SubmitKYC(ctx context.Context, userID, orgID, businessName, tin, docURL string) (KYCSubmission, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return KYCSubmission{}, nil, err
	}
	businessName = strings.TrimSpace(businessName)
	tin = strings.TrimSpace(tin)
	docURL = strings.TrimSpace(docURL)

	errs := validation.Errors{}
	validation.Required(businessName, "Business name is required.", errs, "business_name")
	validation.MaxRunes(businessName, 200, "Business name must be 200 characters or fewer.", errs, "business_name")
	if !validTIN(tin) {
		errs.Add("tin", "TIN must be 9-20 digits.")
	}
	validation.Required(docURL, "An ID document upload is required.", errs, "id_document_url")
	if errs.Any() {
		return KYCSubmission{}, errs, nil
	}
	sub, err := s.repo.SubmitKYC(ctx, strings.TrimSpace(orgID), businessName, tin, docURL)
	return sub, nil, err
}

// GetKYCSubmission returns the evidence row plus live org status for the
// status view (any active member may read).
func (s *Service) GetKYCSubmission(ctx context.Context, userID, orgID string) (KYCSubmission, string, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return KYCSubmission{}, "", err
	}
	sub, found, err := s.repo.GetKYCSubmission(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return KYCSubmission{}, "", err
	}
	if !found {
		return KYCSubmission{}, "", ErrKYCNotSubmitted
	}
	org, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return KYCSubmission{}, "", err
	}
	return sub, org.KYCStatus, nil
}

func (s *Service) ListMembers(ctx context.Context, userID, orgID string) ([]OrgMember, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return nil, err
	}
	return s.repo.ListMembers(ctx, strings.TrimSpace(orgID))
}

func (s *Service) InviteMember(ctx context.Context, actorUserID, orgID, email string, role Role) (OrgMember, error) {
	if _, err := s.CheckOrgPermission(ctx, actorUserID, orgID, PermManageMembers); err != nil {
		return OrgMember{}, err
	}
	if _, err := ParseRole(string(role)); err != nil {
		return OrgMember{}, err
	}
	userID, err := s.repo.FindUserIDByEmail(ctx, email)
	if err != nil {
		return OrgMember{}, err
	}
	if userID == actorUserID {
		return OrgMember{}, ErrAlreadyMember
	}
	// One org per account — invited-but-unaccepted rows don't count.
	if count, err := s.repo.CountActiveOrgsForUser(ctx, userID); err != nil {
		return OrgMember{}, err
	} else if count > 0 {
		return OrgMember{}, ErrSingleOrg
	}
	return s.repo.InviteMember(ctx, strings.TrimSpace(orgID), userID, role, actorUserID)
}

func (s *Service) AcceptInvite(ctx context.Context, userID, orgID string) (OrgMember, error) {
	// Re-check at accept time: the account may have joined an org after
	// the invite was sent (invited rows never count as active).
	if count, err := s.repo.CountActiveOrgsForUser(ctx, userID); err != nil {
		return OrgMember{}, err
	} else if count > 0 {
		return OrgMember{}, ErrSingleOrg
	}
	return s.repo.AcceptInvite(ctx, strings.TrimSpace(orgID), userID)
}

func (s *Service) ChangeMemberRole(ctx context.Context, actorUserID, orgID, targetUserID string, role Role) (OrgMember, error) {
	if _, err := s.CheckOrgPermission(ctx, actorUserID, orgID, PermManageMembers); err != nil {
		return OrgMember{}, err
	}
	if _, err := ParseRole(string(role)); err != nil {
		return OrgMember{}, err
	}
	target, err := s.repo.GetMember(ctx, strings.TrimSpace(orgID), targetUserID)
	if err != nil {
		return OrgMember{}, err
	}
	if target.Role == RoleOwner && role != RoleOwner {
		owners, err := s.repo.CountOwners(ctx, strings.TrimSpace(orgID))
		if err != nil {
			return OrgMember{}, err
		}
		if owners <= 1 {
			return OrgMember{}, ErrLastOwner
		}
	}
	return s.repo.UpdateMemberRole(ctx, strings.TrimSpace(orgID), targetUserID, role)
}

func (s *Service) RemoveMember(ctx context.Context, actorUserID, orgID, targetUserID string) error {
	if _, err := s.CheckOrgPermission(ctx, actorUserID, orgID, PermManageMembers); err != nil {
		return err
	}
	if targetUserID == actorUserID {
		return ErrCannotRemoveSelf
	}
	target, err := s.repo.GetMember(ctx, strings.TrimSpace(orgID), targetUserID)
	if err != nil {
		return err
	}
	if target.Role == RoleOwner {
		owners, err := s.repo.CountOwners(ctx, strings.TrimSpace(orgID))
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	return s.repo.RemoveMember(ctx, strings.TrimSpace(orgID), targetUserID)
}

// OrgIDForApp resolves an app to its organization.
func (s *Service) OrgIDForApp(ctx context.Context, appID string) (string, error) {
	return s.repo.GetAppOrgID(ctx, strings.TrimSpace(appID))
}

// AdminRemoveMember removes any member with the last-owner guard but
// without requiring the actor to be a member (admin tool bypass).
func (s *Service) AdminRemoveMember(ctx context.Context, orgID, targetUserID string) error {
	target, err := s.repo.GetMember(ctx, strings.TrimSpace(orgID), targetUserID)
	if err != nil {
		return err
	}
	if target.Role == RoleOwner {
		owners, err := s.repo.CountOwners(ctx, strings.TrimSpace(orgID))
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	return s.repo.RemoveMember(ctx, strings.TrimSpace(orgID), targetUserID)
}

func (s *Service) LeaveOrganization(ctx context.Context, userID, orgID string) error {
	member, err := s.repo.GetMember(ctx, strings.TrimSpace(orgID), userID)
	if err != nil {
		return ErrNotOrgMember
	}
	if member.Status != MemberStatusActive {
		return ErrNotOrgMember
	}
	if member.Role == RoleOwner {
		owners, err := s.repo.CountOwners(ctx, strings.TrimSpace(orgID))
		if err != nil {
			return err
		}
		if owners <= 1 {
			return ErrLastOwner
		}
	}
	return s.repo.RemoveMember(ctx, strings.TrimSpace(orgID), userID)
}

// AdminAddMember adds an existing user straight to active (admin tool —
// the RequireAdmin gate replaces the owner check). Used to repoint the
// legacy app-member admin endpoints at the org model.
func (s *Service) AdminAddMember(ctx context.Context, orgID, email string, role Role, addedBy string) (OrgMember, error) {
	if _, err := ParseRole(string(role)); err != nil {
		return OrgMember{}, err
	}
	userID, err := s.repo.FindUserIDByEmail(ctx, email)
	if err != nil {
		return OrgMember{}, err
	}
	return s.repo.AddActiveMember(ctx, strings.TrimSpace(orgID), userID, role, addedBy)
}

// ListAppMembers returns active members of an app's org (display + phone
// fields for notifications).
func (s *Service) ListAppMembers(ctx context.Context, appID string) ([]OrgMember, error) {
	return s.repo.ListAppMembers(ctx, strings.TrimSpace(appID))
}
