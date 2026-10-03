package orgs

import (
	"context"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"lipago/internal/shared/notify"
	"lipago/internal/shared/validation"
)

// Service owns organization membership rules. Role enforcement for
// app-scoped handlers lives here too via RoleForApp/Can, so there is one
// decision point no matter which module asks.
type Service struct {
	repo       Repository
	log        *slog.Logger
	dispatcher notify.Dispatcher
}

func (s *Service) SetNotificationDispatcher(dispatcher notify.Dispatcher) { s.dispatcher = dispatcher }

func (s *Service) dispatch(ctx context.Context, eventType, orgID, userID, referenceID string, data map[string]any) {
	if s.dispatcher == nil {
		return
	}
	if data == nil {
		data = map[string]any{}
	}
	if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: eventType, OrgID: orgID, UserID: userID, ReferenceID: referenceID, DedupeKey: eventType + ":" + referenceID, LinkURL: "/settings/verification", Data: data, Source: "system"}); err != nil {
		s.log.Error("dispatch organization notification failed", "event_type", eventType, "org_id", orgID, "error", err)
	}
}

func (s *Service) dispatchAdmins(ctx context.Context, eventType, orgID, referenceID string, data map[string]any) {
	if s.dispatcher == nil {
		return
	}
	if err := s.dispatcher.Dispatch(ctx, notify.Event{EventType: eventType, AllAdmins: true, OrgID: orgID, ReferenceID: referenceID, DedupeKey: eventType + ":" + referenceID, LinkURL: "/admin/kyc", Data: data, Source: "system"}); err != nil {
		s.log.Error("dispatch admin notification failed", "event_type", eventType, "org_id", orgID, "error", err)
	}
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

// ValidHandle reports whether a public creator handle is well-formed:
// 3-30 chars, lowercase letters/digits plus dot/hyphen/underscore.
func ValidHandle(handle string) bool {
	handle = strings.TrimSpace(handle)
	if len(handle) < 3 || len(handle) > 30 {
		return false
	}
	for _, r := range handle {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// NormalizeHandle lowercases and trims a handle for storage/comparison.
func NormalizeHandle(handle string) string {
	return strings.ToLower(strings.TrimSpace(handle))
}

// ValidCreatorIDType reports whether an ID type is accepted for creator KYC.
func ValidCreatorIDType(idType string) bool {
	switch strings.ToLower(strings.TrimSpace(idType)) {
	case "national_id", "passport", "drivers_license", "voters_id":
		return true
	default:
		return false
	}
}

// Creator survey vocab validation (Part 2 onboarding).
func ValidCreatorCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case CreatorCategoryContentCreator, CreatorCategoryMusicianArtist,
		CreatorCategoryFreelancerConsultant, CreatorCategoryCoachEducator,
		CreatorCategoryNonprofitCause, CreatorCategoryPersonalUse, CreatorCategoryOther:
		return true
	default:
		return false
	}
}

func ValidCreatorReferralSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case CreatorReferralSocialMedia, CreatorReferralFriend, CreatorReferralSearch,
		CreatorReferralEvent, CreatorReferralAdvertisement, CreatorReferralOther:
		return true
	default:
		return false
	}
}

func ValidCreatorUseCase(useCase string) bool {
	switch strings.ToLower(strings.TrimSpace(useCase)) {
	case CreatorUseSupportTips, CreatorUseDigitalProducts,
		CreatorUseFreelanceWork, CreatorUseAPIIntegration:
		return true
	default:
		return false
	}
}

func ValidCreatorVolumeBand(band string) bool {
	switch strings.ToLower(strings.TrimSpace(band)) {
	case CreatorVolumeUnder100K, CreatorVolume100KTo1M, CreatorVolume1MTo10M,
		CreatorVolume10MTo100M, CreatorVolumeOver100M:
		return true
	default:
		return false
	}
}

func ValidCreatorTxnBand(band string) bool {
	switch strings.ToLower(strings.TrimSpace(band)) {
	case CreatorTxnUnder50, CreatorTxn50To200, CreatorTxn200To1000,
		CreatorTxn1000To5000, CreatorTxnOver5000:
		return true
	default:
		return false
	}
}

// SuggestedCreatorRiskTier derives the DEFAULT starting risk tier from
// self-reported bands. Segmentation only: a high suggestion flags closer
// admin attention — it never unlocks higher live limits, which still gate
// on kyc_status.
func SuggestedCreatorRiskTier(volumeBand, txnBand string) string {
	volumeBand = strings.ToLower(strings.TrimSpace(volumeBand))
	txnBand = strings.ToLower(strings.TrimSpace(txnBand))
	if volumeBand == CreatorVolumeOver100M || volumeBand == CreatorVolume10MTo100M ||
		txnBand == CreatorTxnOver5000 {
		return CreatorRiskHigh
	}
	if volumeBand == CreatorVolume1MTo10M ||
		txnBand == CreatorTxn200To1000 || txnBand == CreatorTxn1000To5000 {
		return CreatorRiskElevated
	}
	return CreatorRiskStandard
}

// CreateCreatorOrganization creates a creator-kind account: display name +
// unique handle instead of a business name. The creator becomes the single
// owner member; the invite UI stays hidden in v1 (schema still supports a
// later manager add).
func (s *Service) CreateCreatorOrganization(ctx context.Context, userID, name string, in CreatorOrgInput) (Organization, validation.Errors, error) {
	name = strings.TrimSpace(name)
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Handle = NormalizeHandle(in.Handle)
	in.Bio = strings.TrimSpace(in.Bio)

	errs := validation.Errors{}
	validation.Required(name, "Name is required.", errs, "name")
	validation.MaxRunes(name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.Required(in.DisplayName, "Display name is required.", errs, "display_name")
	validation.MaxRunes(in.DisplayName, 100, "Display name must be 100 characters or fewer.", errs, "display_name")
	if !ValidHandle(in.Handle) {
		errs.Add("handle", "Handle must be 3-30 lowercase letters, numbers, dots, hyphens or underscores.")
	} else if ReservedHandle(in.Handle) {
		errs.Add("handle", "That handle is reserved — pick another.")
	}
	validation.MaxRunes(in.Bio, 500, "Bio must be 500 characters or fewer.", errs, "bio")
	if errs.Any() {
		return Organization{}, errs, nil
	}

	if count, err := s.repo.CountActiveOrgsForUser(ctx, userID); err != nil {
		return Organization{}, nil, err
	} else if count > 0 {
		return Organization{}, nil, ErrSingleOrg
	}

	org, err := s.repo.CreateCreatorOrganization(ctx, name, SlugFor(name), in, userID)
	if err != nil {
		return Organization{}, nil, err
	}
	return org, nil, nil
}

// GetCreatorByHandle resolves a public support-page handle. No actor check
// — the handler exposes only the safe public profile subset.
func (s *Service) GetCreatorByHandle(ctx context.Context, handle string) (Organization, error) {
	handle = NormalizeHandle(handle)
	if !ValidHandle(handle) {
		return Organization{}, ErrOrgNotFound
	}
	return s.repo.GetOrganizationByHandle(ctx, handle)
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
	upd.DisplayName = strings.TrimSpace(upd.DisplayName)
	upd.Handle = NormalizeHandle(upd.Handle)
	upd.Bio = strings.TrimSpace(upd.Bio)

	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return Organization{}, nil, err
	}

	errs := validation.Errors{}
	validation.Required(upd.Name, "Name is required.", errs, "name")
	validation.MaxRunes(upd.Name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.MaxRunes(upd.Address, 500, "Address must be 500 characters or fewer.", errs, "address")
	validation.MaxRunes(upd.Phone, 30, "Phone must be 30 characters or fewer.", errs, "phone")
	if upd.ContactEmail != "" && !strings.Contains(upd.ContactEmail, "@") {
		errs.Add("contact_email", "Contact email must be a valid address.")
	}
	validation.MaxRunes(upd.LogoURL, 500, "Logo URL must be 500 characters or fewer.", errs, "logo_url")
	if upd.PrimaryColor != "" && !validHexColor(upd.PrimaryColor) {
		errs.Add("primary_color", "Primary color must be a hex color like #0ea5e9.")
	}
	if current.AccountKind == AccountKindCreator {
		// Creator track: business identity never applies — reject it
		// explicitly instead of silently storing empty strings.
		if upd.BusinessName != "" || upd.TIN != "" {
			errs.Add("business_name", "Creator accounts use individual verification, not a business name/TIN.")
		}
		validation.Required(upd.DisplayName, "Display name is required.", errs, "display_name")
		validation.MaxRunes(upd.DisplayName, 100, "Display name must be 100 characters or fewer.", errs, "display_name")
		if upd.Handle != "" && !ValidHandle(upd.Handle) {
			errs.Add("handle", "Handle must be 3-30 lowercase letters, numbers, dots, hyphens or underscores.")
		} else if upd.Handle != "" && upd.Handle != current.Handle && ReservedHandle(upd.Handle) {
			errs.Add("handle", "That handle is reserved — pick another.")
		}
		validation.MaxRunes(upd.Bio, 500, "Bio must be 500 characters or fewer.", errs, "bio")
	} else {
		validation.MaxRunes(upd.BusinessName, 200, "Business name must be 200 characters or fewer.", errs, "business_name")
		if upd.DisplayName != "" || upd.Handle != "" || upd.Bio != "" {
			errs.Add("display_name", "Display name, handle and bio are creator-only fields.")
		}
	}
	if errs.Any() {
		return Organization{}, errs, nil
	}

	// Verified orgs lock identity: merchants lock business name + TIN,
	// creators lock display identity via re-verification (compare the live
	// KYC submission rather than the org row, which never stores ID data).
	if current.KYCStatus == "verified" {
		if current.AccountKind == AccountKindCreator {
			if sub, found, subErr := s.repo.GetKYCSubmission(ctx, strings.TrimSpace(orgID)); subErr != nil {
				return Organization{}, nil, subErr
			} else if found {
				_ = sub
				// Display identity itself stays editable (it's the public
				// page); the verified legal identity (full name/ID) can
				// only change via resubmission — enforced in
				// SubmitCreatorKYC flipping status back to submitted.
			}
			if upd.Handle != current.Handle || upd.DisplayName != current.DisplayName {
				// Handle/display-name changes are allowed but must stay
				// unique/valid — uniqueness enforced at the DB layer and
				// mapped to ErrHandleTaken by callers.
			}
		} else if upd.BusinessName != current.BusinessName || upd.TIN != current.TIN {
			return Organization{}, nil, ErrReverificationRequired
		}
	}

	org, err := s.repo.UpdateOrganization(ctx, strings.TrimSpace(orgID), upd)
	if err != nil {
		return Organization{}, nil, err
	}
	// Preserve kind on responses from repos that don't populate it (fakes).
	if org.AccountKind == "" {
		org.AccountKind = current.AccountKind
		if org.AccountKind == "" {
			org.AccountKind = AccountKindMerchant
		}
	}
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
		if err == nil {
			s.dispatch(ctx, "kyc.rejected", orgID, "", orgID, map[string]any{"reason": reason})
		}
		return org, nil, err
	}
	org, err := s.repo.ReviewKYC(ctx, orgID, "verified", reviewerEmail, "")
	if err == nil {
		s.dispatch(ctx, "kyc.verified", orgID, "", orgID, nil)
	}
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

// decimalLessThan reports whether a < b for positive decimal strings.
// Unparseable input returns false (callers validate separately).
func decimalLessThan(a, b string) bool {
	ra, okA := new(big.Rat).SetString(strings.TrimSpace(a))
	rb, okB := new(big.Rat).SetString(strings.TrimSpace(b))
	if !okA || !okB {
		return false
	}
	return ra.Cmp(rb) < 0
}

// AdminOrgDetail is the operator's single-org view (linked from
// analytics merchant rows): profile, members, verification evidence,
// history, and the creator onboarding survey (segmentation/risk signal).
// No actor check — route-gated by RequireAdminAuth.
type AdminOrgDetail struct {
	Org      Organization   `json:"org"`
	Members  []OrgMember    `json:"members"`
	KYC      *KYCSubmission `json:"kyc,omitempty"`
	Attempts []KYCAttempt   `json:"attempts"`
	Survey   *CreatorSurvey `json:"survey,omitempty"`
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
	var survey *CreatorSurvey
	if sv, found, err := s.repo.GetCreatorSurvey(ctx, orgID); err != nil {
		return AdminOrgDetail{}, err
	} else if found {
		survey = &sv
	}
	return AdminOrgDetail{Org: org, Members: members, KYC: kyc, Attempts: attempts, Survey: survey}, nil
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
	if err == nil {
		s.dispatch(ctx, "org.limits_changed", orgID, "", orgID, map[string]any{"max_txn": maxTxn, "daily_cap": dailyCap})
	}
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
	if err == nil {
		s.dispatch(ctx, "org.suspended", orgID, "", orgID, map[string]any{"reason": reason})
	}
	return org, nil, err
}

// UnsuspendOrg lifts a suspension (route-gated, audited by the handler).
func (s *Service) UnsuspendOrg(ctx context.Context, orgID string) (Organization, error) {
	orgID = strings.TrimSpace(orgID)
	org, err := s.repo.UnsuspendOrg(ctx, orgID)
	if err == nil {
		s.dispatch(ctx, "org.unsuspended", orgID, "", orgID, nil)
	}
	return org, err
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
// Creator accounts must use SubmitCreatorKYC instead — business KYC is
// rejected explicitly rather than bypassed with empty strings.
func (s *Service) SubmitKYC(ctx context.Context, userID, orgID, businessName, tin, docURL string) (KYCSubmission, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return KYCSubmission{}, nil, err
	}
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return KYCSubmission{}, nil, err
	}
	if current.AccountKind == AccountKindCreator {
		errs := validation.Errors{}
		errs.Add("business_name", "Creator accounts use individual verification — submit full name and ID instead.")
		return KYCSubmission{}, errs, nil
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
	if err == nil {
		s.dispatch(ctx, "kyc.submitted", orgID, "", orgID, nil)
		s.dispatchAdmins(ctx, "admin.kyc_queue", orgID, orgID, map[string]any{"org_id": orgID})
	}
	return sub, nil, err
}

// SubmitCreatorKYC files (or refiles) individual verification evidence for
// creator accounts: full legal name + date of birth (18+) + national ID +
// front document (+ optional back side) + v1 selfie photo. Merchant
// accounts must use SubmitKYC instead.
func (s *Service) SubmitCreatorKYC(ctx context.Context, userID, orgID string, in CreatorKYCInput) (KYCSubmission, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return KYCSubmission{}, nil, err
	}
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return KYCSubmission{}, nil, err
	}
	if current.AccountKind != AccountKindCreator {
		errs := validation.Errors{}
		errs.Add("full_name", "Business accounts use business verification — submit business name and TIN instead.")
		return KYCSubmission{}, errs, nil
	}
	in.FullName = strings.TrimSpace(in.FullName)
	in.IDType = strings.ToLower(strings.TrimSpace(in.IDType))
	in.IDNumber = strings.TrimSpace(in.IDNumber)
	in.Dob = strings.TrimSpace(in.Dob)
	in.DocURL = strings.TrimSpace(in.DocURL)
	in.DocBackURL = strings.TrimSpace(in.DocBackURL)
	in.SelfieURL = strings.TrimSpace(in.SelfieURL)

	errs := validation.Errors{}
	validation.Required(in.FullName, "Full legal name is required.", errs, "full_name")
	validation.MaxRunes(in.FullName, 200, "Full name must be 200 characters or fewer.", errs, "full_name")
	if !ValidCreatorIDType(in.IDType) {
		errs.Add("id_type", "ID type must be national_id, passport, drivers_license, or voters_id.")
	}
	validation.Required(in.IDNumber, "ID number is required.", errs, "id_number")
	validation.MaxRunes(in.IDNumber, 60, "ID number must be 60 characters or fewer.", errs, "id_number")
	// Age-gate: under-18 is a plain validation refusal — progression is
	// blocked before anything is persisted, so no age-identifying reason
	// is ever stored for a rejected signup.
	if !ValidCreatorDOB(in.Dob) {
		errs.Add("dob", "You must be 18 or older to use LipaGO.")
	}
	validation.Required(in.DocURL, "An ID document upload is required.", errs, "id_document_url")
	validation.MaxRunes(in.DocBackURL, 500, "Back-side document reference must be 500 characters or fewer.", errs, "id_document_back_url")
	validation.Required(in.SelfieURL, "A selfie photo is required.", errs, "selfie_url")
	if errs.Any() {
		return KYCSubmission{}, errs, nil
	}
	sub, err := s.repo.SubmitCreatorKYC(ctx, strings.TrimSpace(orgID), in)
	if err == nil {
		s.dispatch(ctx, "kyc.submitted", orgID, "", orgID, nil)
		s.dispatchAdmins(ctx, "admin.kyc_queue", orgID, orgID, map[string]any{"org_id": orgID})
	}
	return sub, nil, err
}

// ValidCreatorDOB reports whether value is a YYYY-MM-DD date proving the
// holder is at least 18 years old (and not implausibly old). Pure
// function so the gate is unit-testable without a database.
func ValidCreatorDOB(value string) bool {
	dob, err := time.Parse("2006-01-02", strings.TrimSpace(value))
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if dob.After(today) || dob.Year() < 1900 {
		return false
	}
	eighteen := time.Date(today.Year()-18, today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	return !dob.After(eighteen)
}

// SaveCreatorSurvey stores (or replaces) a creator org's onboarding
// answers. Creator accounts only; any active member may read, but only
// manage_org may write. Display name (Q1) is applied to the org row in
// the same call so the survey form submits once.
func (s *Service) SaveCreatorSurvey(ctx context.Context, userID, orgID string, in CreatorSurveyInput) (CreatorSurvey, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return CreatorSurvey{}, nil, err
	}
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return CreatorSurvey{}, nil, err
	}
	if current.AccountKind != AccountKindCreator {
		errs := validation.Errors{}
		errs.Add("category", "Onboarding surveys are for creator accounts only.")
		return CreatorSurvey{}, errs, nil
	}
	in.DisplayName = strings.TrimSpace(in.DisplayName)
	in.Category = strings.ToLower(strings.TrimSpace(in.Category))
	in.CategoryOther = strings.TrimSpace(in.CategoryOther)
	in.ReferralSource = strings.ToLower(strings.TrimSpace(in.ReferralSource))
	in.ExpectedVolumeBand = strings.ToLower(strings.TrimSpace(in.ExpectedVolumeBand))
	in.ExpectedTxnBand = strings.ToLower(strings.TrimSpace(in.ExpectedTxnBand))
	normalizedUseCases := []string{}
	seenUseCases := map[string]bool{}
	for _, u := range in.UseCases {
		u = strings.ToLower(strings.TrimSpace(u))
		if u == "" || seenUseCases[u] {
			continue
		}
		seenUseCases[u] = true
		normalizedUseCases = append(normalizedUseCases, u)
	}
	in.UseCases = normalizedUseCases

	errs := validation.Errors{}
	if in.DisplayName != "" {
		validation.MaxRunes(in.DisplayName, 100, "Display name must be 100 characters or fewer.", errs, "display_name")
	}
	if !ValidCreatorCategory(in.Category) {
		errs.Add("category", "Category must be content_creator, musician_artist, freelancer_consultant, coach_educator, nonprofit_cause, personal_use, or other.")
	}
	if in.Category == CreatorCategoryOther {
		validation.Required(in.CategoryOther, "Describe your category when selecting Other.", errs, "category_other")
		validation.MaxRunes(in.CategoryOther, 200, "Category description must be 200 characters or fewer.", errs, "category_other")
	}
	if !ValidCreatorReferralSource(in.ReferralSource) {
		errs.Add("referral_source", "Referral source must be social_media, friend_colleague, search_engine, event_conference, advertisement, or other.")
	}
	if len(in.UseCases) == 0 {
		errs.Add("use_cases", "Select at least one use case.")
	}
	for _, u := range in.UseCases {
		if !ValidCreatorUseCase(u) {
			errs.Add("use_cases", "Use cases must be support_tips, digital_products, freelance_work, or api_integration.")
			break
		}
	}
	if !ValidCreatorVolumeBand(in.ExpectedVolumeBand) {
		errs.Add("expected_volume_band", "Expected monthly amount must be under_100k, 100k_1m, 1m_10m, 10m_100m, or over_100m.")
	}
	if !ValidCreatorTxnBand(in.ExpectedTxnBand) {
		errs.Add("expected_txn_band", "Expected monthly payments must be under_50, 50_200, 200_1000, 1000_5000, or over_5000.")
	}
	if errs.Any() {
		return CreatorSurvey{}, errs, nil
	}
	survey, err := s.repo.UpsertCreatorSurvey(ctx, strings.TrimSpace(orgID), in)
	return survey, nil, err
}

// GetCreatorSurvey returns a creator org's onboarding answers plus the
// derived starting risk tier (any active member may read).
func (s *Service) GetCreatorSurvey(ctx context.Context, userID, orgID string) (CreatorSurvey, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return CreatorSurvey{}, err
	}
	survey, found, err := s.repo.GetCreatorSurvey(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return CreatorSurvey{}, err
	}
	if !found {
		return CreatorSurvey{}, ErrSurveyNotFound
	}
	return survey, nil
}

// SwitchCreatorToMerchant moves a pre-KYC creator org to the merchant
// track (the Q4 "API integration" escape hatch). Refused once any KYC
// submission exists — after that, kind changes are support-assisted.
func (s *Service) SwitchCreatorToMerchant(ctx context.Context, userID, orgID string) (Organization, error) {
	// Account kind is immutable. Keep this method only as a compatibility
	// surface for old callers; never reach the repository conversion path.
	return Organization{}, ErrKindSwitchDisabled
}

// SupportPageData is everything the public support page needs that is
// safe to display: profile, category badge source, page config, buttons.
// Never identity, contact, or KYC data. No actor check — served publicly.
type SupportPageData struct {
	Org      Organization    `json:"org"`
	Category string          `json:"category"`
	Settings SupportSettings `json:"settings"`
	Enabled  bool            `json:"enabled"`
}

// PublicCreatorSupport resolves a handle to its public page data. Unknown
// handles and non-creator rows surface as not-found so the handle
// namespace cannot be probed for merchant orgs. Suspended orgs also
// 404: a support page must never solicit money for a frozen account.
func (s *Service) PublicCreatorSupport(ctx context.Context, handle string) (SupportPageData, error) {
	org, err := s.repo.GetOrganizationByHandle(ctx, handle)
	if err != nil {
		return SupportPageData{}, err
	}
	if org.Suspended {
		return SupportPageData{}, ErrOrgNotFound
	}
	category := ""
	if survey, found, err := s.repo.GetCreatorSurvey(ctx, org.ID); err != nil {
		return SupportPageData{}, err
	} else if found {
		category = survey.Category
	}
	settings, found, err := s.repo.GetSupportSettings(ctx, org.ID)
	if err != nil {
		return SupportPageData{}, err
	}
	if !found {
		settings = SupportSettings{OrgID: org.ID, Links: []SupportLink{}}
	}
	return SupportPageData{
		Org:      org,
		Category: category,
		Settings: settings,
		Enabled:  found && strings.TrimSpace(settings.SupportAppID) != "",
	}, nil
}

// GetSupportSettings returns a creator org's page configuration (any
// active member may read).
func (s *Service) GetSupportSettings(ctx context.Context, userID, orgID string) (SupportSettings, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermRead); err != nil {
		return SupportSettings{}, err
	}
	settings, found, err := s.repo.GetSupportSettings(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return SupportSettings{}, err
	}
	if !found {
		return SupportSettings{OrgID: strings.TrimSpace(orgID), Links: []SupportLink{}}, nil
	}
	return settings, nil
}

// SaveSupportSettings replaces a creator org's page configuration
// (owner/manage_org). Bounds are sanity-checked here; they are clamped
// against the live per-transaction cap at order time, and self-reported
// values can never raise live limits. The supporters-wall toggle is
// future scope — writes are accepted but the wall is never rendered
// publicly until that toggle ships (always off for now).
func (s *Service) SaveSupportSettings(ctx context.Context, userID, orgID string, in SupportSettingsInput) (SupportSettings, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return SupportSettings{}, nil, err
	}
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return SupportSettings{}, nil, err
	}
	if current.AccountKind != AccountKindCreator {
		errs := validation.Errors{}
		errs.Add("support_app_id", "Support pages are for creator accounts only.")
		return SupportSettings{}, errs, nil
	}
	in.SupportAppID = strings.TrimSpace(in.SupportAppID)
	in.MinAmount = strings.TrimSpace(in.MinAmount)
	in.MaxAmount = strings.TrimSpace(in.MaxAmount)

	errs := validation.Errors{}
	if in.MinAmount != "" && !validPositiveDecimal(in.MinAmount) {
		errs.Add("min_amount", "Minimum amount must be a positive number.")
	}
	if in.MaxAmount != "" && !validPositiveDecimal(in.MaxAmount) {
		errs.Add("max_amount", "Maximum amount must be a positive number.")
	}
	if in.MinAmount != "" && validPositiveDecimal(in.MinAmount) && decimalLessThan(in.MinAmount, CreatorSupportMinAmount) {
		errs.Add("min_amount", "Minimum amount is at least "+CreatorSupportMinAmount+" TZS (dust protection).")
	}
	if in.MinAmount != "" && in.MaxAmount != "" && validPositiveDecimal(in.MinAmount) && validPositiveDecimal(in.MaxAmount) &&
		!decimalLessThan(in.MinAmount, in.MaxAmount) {
		errs.Add("max_amount", "Maximum amount must be above the minimum.")
	}
	links := make([]SupportLinkInput, 0, len(in.Links))
	if len(in.Links) > maxSupportLinks {
		errs.Add("links", "At most 6 support buttons.")
	}
	for i, l := range in.Links {
		if i >= maxSupportLinks {
			break
		}
		l.Label = strings.TrimSpace(l.Label)
		l.AmountMode = strings.ToLower(strings.TrimSpace(l.AmountMode))
		l.Amount = strings.TrimSpace(l.Amount)
		if l.Label == "" {
			errs.Add("links", "Every button needs a label.")
			continue
		}
		validation.MaxRunes(l.Label, 60, "Button labels must be 60 characters or fewer.", errs, "links")
		if l.AmountMode != "fixed" && l.AmountMode != "open" {
			errs.Add("links", "Button amount mode must be fixed or open.")
			continue
		}
		if l.AmountMode == "fixed" {
			if strings.TrimSpace(l.Amount) == "" || !validPositiveDecimal(l.Amount) {
				errs.Add("links", "Fixed-amount buttons need a positive amount.")
				continue
			}
		} else {
			l.Amount = ""
		}
		l.Active = true
		links = append(links, l)
	}
	if errs.Any() {
		return SupportSettings{}, errs, nil
	}
	in.Links = links
	settings, err := s.repo.UpsertSupportSettings(ctx, strings.TrimSpace(orgID), in)
	return settings, nil, err
}

// EnsureSupportPage links the receiving app to the creator's page and
// seeds the default buttons when the page has none. Called by the
// enable flow after the app is created via the single CreateApp path.
func (s *Service) EnsureSupportPage(ctx context.Context, userID, orgID, appID string) (SupportSettings, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return SupportSettings{}, err
	}
	current, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return SupportSettings{}, err
	}
	if current.AccountKind != AccountKindCreator {
		return SupportSettings{}, ErrSupportPageDisabled
	}
	settings, found, err := s.repo.GetSupportSettings(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return SupportSettings{}, err
	}
	links := []SupportLinkInput{}
	if found {
		if strings.TrimSpace(settings.SupportAppID) != "" {
			return settings, nil
		}
		for _, l := range settings.Links {
			links = append(links, SupportLinkInput{Label: l.Label, AmountMode: l.AmountMode, Amount: l.Amount, Active: l.Active})
		}
	}
	if len(links) == 0 {
		links = defaultSupportLinks()
	}
	updated, err := s.repo.UpsertSupportSettings(ctx, strings.TrimSpace(orgID), SupportSettingsInput{
		SupportAppID: appID,
		MinAmount:    settings.MinAmount,
		MaxAmount:    settings.MaxAmount,
		Links:        links,
	})
	return updated, err
}

// RequireAccountKind enforces a membership permission and the account
// track in one call — the reusable kind gate for all kind-specific
// endpoints. Shared infrastructure (payment processing, ledger, webhook
// delivery, admin review) skips it; kind-specific endpoints 403 the wrong
// kind instead of silently no-op-ing. Kind is always resolved from the
// database here, never trusted from the client.
func (s *Service) RequireAccountKind(ctx context.Context, userID, orgID string, perm Permission, kinds ...string) (Organization, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, perm); err != nil {
		return Organization{}, err
	}
	org, err := s.repo.GetOrganization(ctx, strings.TrimSpace(orgID))
	if err != nil {
		return Organization{}, err
	}
	kind := strings.TrimSpace(org.AccountKind)
	if kind == "" {
		kind = AccountKindMerchant
	}
	for _, want := range kinds {
		if kind == want {
			return org, nil
		}
	}
	if len(kinds) == 1 && kinds[0] == AccountKindCreator {
		return Organization{}, ErrNotCreatorOrg
	}
	if len(kinds) == 1 && kinds[0] == AccountKindMerchant {
		return Organization{}, ErrNotMerchantOrg
	}
	return Organization{}, ErrNotCreatorOrg
}

// RequireCreatorOrg enforces a membership permission and the creator
// track in one call for support-page management routes.
func (s *Service) RequireCreatorOrg(ctx context.Context, userID, orgID string, perm Permission) (Organization, error) {
	return s.RequireAccountKind(ctx, userID, orgID, perm, AccountKindCreator)
}

// RequireMerchantOrg enforces a membership permission and the merchant
// (business) track in one call for team-management routes. Creator
// accounts are personal single-member workspaces — invites, role changes
// and removals are refused with 403, never silently accepted.
func (s *Service) RequireMerchantOrg(ctx context.Context, userID, orgID string, perm Permission) (Organization, error) {
	return s.RequireAccountKind(ctx, userID, orgID, perm, AccountKindMerchant)
}

// requireKindApp resolves an app to its org, enforces one permission from
// the central matrix, then enforces the account track — the reusable gate
// for app-scoped money-plane endpoints in both tracks. Kind is resolved
// from the database alongside the membership, never trusted from the
// client. It returns the owning org id for handlers that need it.
func (s *Service) requireKindApp(ctx context.Context, userID, appID string, perm Permission, kinds ...string) (string, error) {
	if _, err := s.CheckAppPermission(ctx, userID, appID, perm); err != nil {
		return "", err
	}
	orgID, err := s.repo.GetAppOrgID(ctx, strings.TrimSpace(appID))
	if err != nil {
		return "", ErrNotOrgMember
	}
	org, err := s.repo.GetOrganization(ctx, orgID)
	if err != nil {
		return "", err
	}
	kind := strings.TrimSpace(org.AccountKind)
	if kind == "" {
		kind = AccountKindMerchant
	}
	for _, want := range kinds {
		if kind == want {
			return orgID, nil
		}
	}
	if len(kinds) == 1 && kinds[0] == AccountKindMerchant {
		return "", ErrNotMerchantOrg
	}
	return "", ErrNotCreatorOrg
}

// RequireMerchantApp enforces a membership permission and the merchant
// track for one app — the gate for every /api/v1/merchant/apps/* handler.
func (s *Service) RequireMerchantApp(ctx context.Context, userID, appID string, perm Permission) (string, error) {
	return s.requireKindApp(ctx, userID, appID, perm, AccountKindMerchant)
}

// RequireCreatorApp enforces a membership permission and the creator
// track for one app — the gate for every /api/v1/creator/apps/* handler.
func (s *Service) RequireCreatorApp(ctx context.Context, userID, appID string, perm Permission) (string, error) {
	return s.requireKindApp(ctx, userID, appID, perm, AccountKindCreator)
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
	if _, err := s.RequireMerchantOrg(ctx, actorUserID, orgID, PermManageMembers); err != nil {
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
	member, err := s.repo.InviteMember(ctx, strings.TrimSpace(orgID), userID, role, actorUserID)
	if err == nil {
		s.dispatch(ctx, "org.member_invited", orgID, member.UserID, orgID+":"+member.UserID, map[string]any{"role": role})
	}
	return member, err
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
	if _, err := s.RequireMerchantOrg(ctx, actorUserID, orgID, PermManageMembers); err != nil {
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
	member, err := s.repo.UpdateMemberRole(ctx, strings.TrimSpace(orgID), targetUserID, role)
	if err == nil {
		s.dispatch(ctx, "org.role_changed", orgID, targetUserID, orgID+":"+targetUserID+":"+string(role), map[string]any{"role": role})
	}
	return member, err
}

func (s *Service) RemoveMember(ctx context.Context, actorUserID, orgID, targetUserID string) error {
	if _, err := s.CheckOrgPermission(ctx, actorUserID, orgID, PermManageMembers); err != nil {
		return err
	}
	if _, err := s.RequireMerchantOrg(ctx, actorUserID, orgID, PermManageMembers); err != nil {
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
