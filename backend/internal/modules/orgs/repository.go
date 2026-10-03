package orgs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx"
)

// Repository is the persistence boundary for organizations. Kept as an
// interface so service rules unit-test against a fake.
type Repository interface {
	CreateOrganization(ctx context.Context, name, slug, businessName, ownerUserID string) (Organization, error)
	GetOrganization(ctx context.Context, orgID string) (Organization, error)
	ListOrganizationsForUser(ctx context.Context, userID string) ([]OrganizationWithRole, error)
	UpdateOrganization(ctx context.Context, orgID string, upd OrgProfileUpdate) (Organization, error)
	DeleteOrganization(ctx context.Context, orgID string) error
	CountApps(ctx context.Context, orgID string) (int64, error)
	GetAppOrgID(ctx context.Context, appID string) (string, error)
	GetMember(ctx context.Context, orgID, userID string) (OrgMember, error)
	ListMembers(ctx context.Context, orgID string) ([]OrgMember, error)
	InviteMember(ctx context.Context, orgID, userID string, role Role, invitedBy string) (OrgMember, error)
	AcceptInvite(ctx context.Context, orgID, userID string) (OrgMember, error)
	AddActiveMember(ctx context.Context, orgID, userID string, role Role, addedBy string) (OrgMember, error)
	ListAppMembers(ctx context.Context, appID string) ([]OrgMember, error)
	UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) (OrgMember, error)
	RemoveMember(ctx context.Context, orgID, userID string) error
	CountOwners(ctx context.Context, orgID string) (int64, error)
	FindUserIDByEmail(ctx context.Context, email string) (string, error)
	CountActiveMembers(ctx context.Context, orgID string) (int64, error)
	// CountActiveOrgsForUser counts ACTIVE memberships of a user across all
	// orgs. Product rule: one org per account — enforced in the service
	// before creating or joining another org.
	CountActiveOrgsForUser(ctx context.Context, userID string) (int64, error)
	SubmitKYC(ctx context.Context, orgID, businessName, tin, docURL string) (KYCSubmission, error)
	GetKYCSubmission(ctx context.Context, orgID string) (KYCSubmission, bool, error)
	// ReviewKYC flips kyc_status (verified|rejected) and records the
	// review on the submission. Only submitted orgs are reviewable.
	ReviewKYC(ctx context.Context, orgID, status, reviewedBy, reason string) (Organization, error)
	// ListKYCQueue returns orgs with submissions, newest first. Empty
	// status means submitted (the actionable queue); verified/rejected
	// give history.
	ListKYCQueue(ctx context.Context, status string) ([]KYCQueueItem, error)
	// UpdateOrgLiveLimits sets per-org live caps (empty clears to
	// platform default).
	UpdateOrgLiveLimits(ctx context.Context, orgID, maxTxn, dailyCap string) (Organization, error)
	// ListKYCAttempts returns the immutable submit/decide history, newest
	// first (Settings verification tab).
	ListKYCAttempts(ctx context.Context, orgID string) ([]KYCAttempt, error)
	// GetNotificationPrefs returns the org's toggles (all-enabled when no
	// row was ever saved).
	GetNotificationPrefs(ctx context.Context, orgID string) (NotificationPrefs, error)
	// UpdateOrgLogo sets the org's logo location (uploaded path or URL).
	UpdateOrgLogo(ctx context.Context, orgID, logoURL string) (Organization, error)
	// SuspendOrg freezes live money movement (reason required); UnsuspendOrg
	// lifts it. Both are admin-only and audited by the caller.
	SuspendOrg(ctx context.Context, orgID, reason string) (Organization, error)
	UnsuspendOrg(ctx context.Context, orgID string) (Organization, error)
	// OrgSuspensionByApp resolves an app to its org's suspension state for
	// money-movement gates.
	OrgSuspensionByApp(ctx context.Context, appID string) (suspended bool, reason string, err error)
	// WriteAudit records an admin mutation (best-effort by convention).
	WriteAudit(ctx context.Context, actorID, actorEmail, action, targetType, targetID, ip string, before, after map[string]any) error
	// ListAudit returns the trail newest-first (admin viewer).
	ListAudit(ctx context.Context, action, actor string, limit, offset int) ([]AuditEntry, error)
	// UpsertNotificationPrefs replaces the org's toggles.
	UpsertNotificationPrefs(ctx context.Context, prefs NotificationPrefs) (NotificationPrefs, error)
	// PlatformStats counts tenants and workload for the admin dashboard.
	PlatformStats(ctx context.Context) (PlatformStats, error)
	// CreateCreatorOrganization inserts a creator-kind org + owner member.
	CreateCreatorOrganization(ctx context.Context, name, slug string, in CreatorOrgInput, ownerUserID string) (Organization, error)
	// SubmitCreatorKYC files individual verification evidence for creators.
	SubmitCreatorKYC(ctx context.Context, orgID string, in CreatorKYCInput) (KYCSubmission, error)
	// GetOrganizationByHandle resolves a public creator handle (case-insensitive).
	GetOrganizationByHandle(ctx context.Context, handle string) (Organization, error)
	// UpsertCreatorSurvey stores (or replaces) a creator org's onboarding answers.
	UpsertCreatorSurvey(ctx context.Context, orgID string, in CreatorSurveyInput) (CreatorSurvey, error)
	// GetCreatorSurvey loads a creator org's onboarding answers; found=false when never submitted.
	GetCreatorSurvey(ctx context.Context, orgID string) (CreatorSurvey, bool, error)
	// SwitchCreatorToMerchant converts a pre-KYC creator org to the merchant
	// track (clears creator profile + survey). Only before any KYC
	// submission exists.
	SwitchCreatorToMerchant(ctx context.Context, orgID string) (Organization, error)
	// GetSupportSettings loads a creator org's public-page configuration;
	// found=false when the page was never enabled.
	GetSupportSettings(ctx context.Context, orgID string) (SupportSettings, bool, error)
	// UpsertSupportSettings replaces the page configuration and its
	// buttons in one transaction.
	UpsertSupportSettings(ctx context.Context, orgID string, in SupportSettingsInput) (SupportSettings, error)
}

type PostgresRepository struct {
	db *pgx.ConnPool
}

func NewPostgresRepository(db *pgx.ConnPool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func scanOrganization(row interface {
	Scan(dest ...interface{}) error
}) (Organization, error) {
	var org Organization
	var businessName, tin, maxTxn, dailyCap sql.NullString
	var address, phone, contactEmail, logoURL, primaryColor sql.NullString
	var suspendedReason sql.NullString
	var accountKind sql.NullString
	var displayName, handle, bio sql.NullString
	if err := row.Scan(
		&org.ID, &org.Name, &org.Slug, &org.KYCStatus,
		&businessName, &tin, &maxTxn, &dailyCap,
		&address, &phone, &contactEmail, &logoURL, &primaryColor,
		&org.Suspended, &suspendedReason,
		&accountKind, &displayName, &handle, &bio,
		&org.CreatedAt, &org.UpdatedAt,
	); err != nil {
		return Organization{}, err
	}
	org.BusinessName = businessName.String
	org.TIN = tin.String
	org.LiveMaxTxnAmount = maxTxn.String
	org.LiveDailyVolumeCap = dailyCap.String
	org.Address = address.String
	org.Phone = phone.String
	org.ContactEmail = contactEmail.String
	org.LogoURL = logoURL.String
	org.PrimaryColor = primaryColor.String
	org.SuspendedReason = suspendedReason.String
	org.AccountKind = accountKind.String
	if org.AccountKind == "" {
		org.AccountKind = AccountKindMerchant
	}
	org.DisplayName = displayName.String
	org.Handle = handle.String
	org.Bio = bio.String
	return org, nil
}

const organizationSelect = `
	SELECT id::text, name, slug, kyc_status,
	       business_name, tin, live_max_txn_amount, live_daily_volume_cap,
	       address, phone, contact_email, logo_url, primary_color,
	       suspended, COALESCE(suspended_reason, ''),
	       COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
	       COALESCE(handle, ''), COALESCE(bio, ''),
	       created_at, updated_at
	FROM app.organizations
`

func scanOrgMember(row interface{ Scan(dest ...interface{}) error }) (OrgMember, error) {
	var member OrgMember
	var email, fullName, phone sql.NullString
	var role, status string
	if err := row.Scan(
		&member.OrgID, &member.UserID, &email, &fullName, &phone,
		&role, &member.InvitedBy, &status, &member.CreatedAt,
	); err != nil {
		return OrgMember{}, err
	}
	member.Email = email.String
	member.FullName = fullName.String
	member.Phone = phone.String
	member.Role = Role(role)
	member.Status = MemberStatus(status)
	return member, nil
}

// SlugFor generates a URL-safe unique slug for a new org.
func SlugFor(name string) string {
	base := strings.ToLower(strings.TrimSpace(name))
	var cleaned strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			cleaned.WriteRune('-')
		}
	}
	slug := strings.Trim(cleaned.String(), "-")
	if len(slug) > 40 {
		slug = slug[:40]
	}
	if slug == "" {
		slug = "org"
	}
	suffix := make([]byte, 3)
	if _, err := rand.Read(suffix); err != nil {
		return slug + "-000000"
	}
	return slug + "-" + hex.EncodeToString(suffix)
}

func (r *PostgresRepository) CreateOrganization(ctx context.Context, name, slug, businessName, ownerUserID string) (Organization, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return Organization{}, fmt.Errorf("begin create organization transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	var org Organization
	org, err = scanOrganization(tx.QueryRowEx(ctx, `
		INSERT INTO app.organizations (name, slug, business_name)
		VALUES ($1, $2, $3)
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, name, slug, valueOrNil(businessName)))
	if err != nil {
		return Organization{}, fmt.Errorf("insert organization: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.org_members (org_id, user_id, role, invited_by, status)
		VALUES ($1::uuid, $2::uuid, 'owner', $2, 'active')
	`, nil, org.ID, ownerUserID); err != nil {
		return Organization{}, fmt.Errorf("insert owner membership: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit create organization transaction: %w", err)
	}
	return org, nil
}

func valueOrNil(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func (r *PostgresRepository) GetOrganization(ctx context.Context, orgID string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, organizationSelect+` WHERE id = $1::uuid`, nil, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("get organization: %w", err)
	}
	return org, nil
}

func (r *PostgresRepository) ListOrganizationsForUser(ctx context.Context, userID string) ([]OrganizationWithRole, error) {
	rows, err := r.db.QueryEx(ctx, `
		SELECT o.id::text, o.name, o.slug, o.kyc_status,
		       o.business_name, o.tin, o.live_max_txn_amount, o.live_daily_volume_cap,
		       o.address, o.phone, o.contact_email, o.logo_url, o.primary_color,
		       o.suspended, COALESCE(o.suspended_reason, ''),
		       COALESCE(o.account_kind, 'merchant'), COALESCE(o.display_name, ''),
		       COALESCE(o.handle, ''), COALESCE(o.bio, ''),
		       o.created_at, o.updated_at,
		       m.role, m.status
		FROM app.org_members m
		JOIN app.organizations o ON o.id = m.org_id
		WHERE m.user_id = $1::uuid
		ORDER BY o.created_at ASC
	`, nil, userID)
	if err != nil {
		return nil, fmt.Errorf("list organizations for user: %w", err)
	}
	defer rows.Close()

	orgs := []OrganizationWithRole{}
	for rows.Next() {
		var item OrganizationWithRole
		var businessName, tin, maxTxn, dailyCap sql.NullString
		var address, phone, contactEmail, logoURL, primaryColor sql.NullString
		var suspendedReason sql.NullString
		var accountKind, displayName, handle, bio sql.NullString
		var role, status string
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.KYCStatus,
			&businessName, &tin, &maxTxn, &dailyCap,
			&address, &phone, &contactEmail, &logoURL, &primaryColor,
			&item.Suspended, &suspendedReason,
			&accountKind, &displayName, &handle, &bio,
			&item.CreatedAt, &item.UpdatedAt,
			&role, &status,
		); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		item.BusinessName = businessName.String
		item.TIN = tin.String
		item.LiveMaxTxnAmount = maxTxn.String
		item.LiveDailyVolumeCap = dailyCap.String
		item.Address = address.String
		item.Phone = phone.String
		item.ContactEmail = contactEmail.String
		item.LogoURL = logoURL.String
		item.PrimaryColor = primaryColor.String
		item.SuspendedReason = suspendedReason.String
		item.AccountKind = accountKind.String
		if item.AccountKind == "" {
			item.AccountKind = AccountKindMerchant
		}
		item.DisplayName = displayName.String
		item.Handle = handle.String
		item.Bio = bio.String
		item.Role = Role(role)
		item.Status = MemberStatus(status)
		orgs = append(orgs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organizations: %w", err)
	}
	return orgs, nil
}

func (r *PostgresRepository) UpdateOrganization(ctx context.Context, orgID string, upd OrgProfileUpdate) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations
		SET name = $2, business_name = $3, tin = $4, address = $5, phone = $6,
		    contact_email = $7, logo_url = $8, primary_color = $9,
		    display_name = $10, handle = $11, bio = $12, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID, upd.Name, valueOrNil(upd.BusinessName), valueOrNil(upd.TIN),
		valueOrNil(upd.Address), valueOrNil(upd.Phone), valueOrNil(upd.ContactEmail),
		valueOrNil(upd.LogoURL), valueOrNil(upd.PrimaryColor),
		valueOrNil(upd.DisplayName), valueOrNil(upd.Handle), valueOrNil(upd.Bio)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("update organization: %w", err)
	}
	return org, nil
}

func (r *PostgresRepository) DeleteOrganization(ctx context.Context, orgID string) error {
	tag, err := r.db.ExecEx(ctx, `DELETE FROM app.organizations WHERE id = $1::uuid`, nil, orgID)
	if err != nil {
		return fmt.Errorf("delete organization: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrOrgNotFound
	}
	return nil
}

func (r *PostgresRepository) CountApps(ctx context.Context, orgID string) (int64, error) {
	var count int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.payment_apps WHERE org_id = $1::uuid AND status != 'deleted'`, nil, orgID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count org apps: %w", err)
	}
	return count, nil
}

func (r *PostgresRepository) GetAppOrgID(ctx context.Context, appID string) (string, error) {
	var orgID string
	err := r.db.QueryRowEx(ctx, `SELECT org_id::text FROM app.payment_apps WHERE id = $1::uuid`, nil, appID).Scan(&orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrOrgNotFound
	}
	if err != nil {
		return "", fmt.Errorf("get app org: %w", err)
	}
	return orgID, nil
}

const orgMemberSelect = `
	SELECT m.org_id::text, m.user_id::text, u.email, u.full_name, u.phone,
	       m.role, m.invited_by, m.status, m.created_at
	FROM app.org_members m
	LEFT JOIN app.users u ON u.id = m.user_id
`

func (r *PostgresRepository) GetMember(ctx context.Context, orgID, userID string) (OrgMember, error) {
	member, err := scanOrgMember(r.db.QueryRowEx(ctx, orgMemberSelect+` WHERE m.org_id = $1::uuid AND m.user_id = $2::uuid`, nil, orgID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgMember{}, ErrNotOrgMember
	}
	if err != nil {
		return OrgMember{}, fmt.Errorf("get org member: %w", err)
	}
	return member, nil
}

func (r *PostgresRepository) ListMembers(ctx context.Context, orgID string) ([]OrgMember, error) {
	rows, err := r.db.QueryEx(ctx, orgMemberSelect+` WHERE m.org_id = $1::uuid ORDER BY m.created_at ASC`, nil, orgID)
	if err != nil {
		return nil, fmt.Errorf("list org members: %w", err)
	}
	defer rows.Close()

	members := []OrgMember{}
	for rows.Next() {
		member, err := scanOrgMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan org member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate org members: %w", err)
	}
	return members, nil
}

func (r *PostgresRepository) InviteMember(ctx context.Context, orgID, userID string, role Role, invitedBy string) (OrgMember, error) {
	member, err := scanOrgMember(r.db.QueryRowEx(ctx, `
		INSERT INTO app.org_members (org_id, user_id, role, invited_by, status)
		VALUES ($1::uuid, $2::uuid, $3, $4, 'invited')
		RETURNING org_id::text, user_id::text, ''::text, ''::text, ''::text, role, invited_by, status, created_at
	`, nil, orgID, userID, string(role), invitedBy))
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return OrgMember{}, ErrAlreadyMember
		}
		return OrgMember{}, fmt.Errorf("invite org member: %w", err)
	}
	return member, nil
}

func (r *PostgresRepository) AcceptInvite(ctx context.Context, orgID, userID string) (OrgMember, error) {
	member, err := scanOrgMember(r.db.QueryRowEx(ctx, `
		UPDATE app.org_members SET status = 'active'
		WHERE org_id = $1::uuid AND user_id = $2::uuid AND status = 'invited'
		RETURNING org_id::text, user_id::text, ''::text, ''::text, ''::text, role, invited_by, status, created_at
	`, nil, orgID, userID))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgMember{}, ErrInviteNotFound
	}
	if err != nil {
		return OrgMember{}, fmt.Errorf("accept org invite: %w", err)
	}
	return member, nil
}

func (r *PostgresRepository) UpdateMemberRole(ctx context.Context, orgID, userID string, role Role) (OrgMember, error) {
	member, err := scanOrgMember(r.db.QueryRowEx(ctx, `
		UPDATE app.org_members SET role = $3
		WHERE org_id = $1::uuid AND user_id = $2::uuid AND status = 'active'
		RETURNING org_id::text, user_id::text, ''::text, ''::text, ''::text, role, invited_by, status, created_at
	`, nil, orgID, userID, string(role)))
	if errors.Is(err, pgx.ErrNoRows) {
		return OrgMember{}, ErrNotOrgMember
	}
	if err != nil {
		return OrgMember{}, fmt.Errorf("update org member role: %w", err)
	}
	return member, nil
}

// AddActiveMember inserts an immediately-active membership (admin add
// path). Unlike invites, no accept step is needed.
func (r *PostgresRepository) AddActiveMember(ctx context.Context, orgID, userID string, role Role, addedBy string) (OrgMember, error) {
	member, err := scanOrgMember(r.db.QueryRowEx(ctx, `
		INSERT INTO app.org_members (org_id, user_id, role, invited_by, status)
		VALUES ($1::uuid, $2::uuid, $3, $4, 'active')
		RETURNING org_id::text, user_id::text, ''::text, ''::text, ''::text, role, invited_by, status, created_at
	`, nil, orgID, userID, string(role), addedBy))
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return OrgMember{}, ErrAlreadyMember
		}
		return OrgMember{}, fmt.Errorf("add org member: %w", err)
	}
	return member, nil
}

// ListAppMembers lists active members of an app's org with display fields
// (powers success-SMS phone lookup among others).
func (r *PostgresRepository) ListAppMembers(ctx context.Context, appID string) ([]OrgMember, error) {
	rows, err := r.db.QueryEx(ctx, `
		SELECT m.org_id::text, m.user_id::text, u.email, u.full_name, u.phone,
		       m.role, m.invited_by, m.status, m.created_at
		FROM app.payment_apps a
		JOIN app.org_members m ON m.org_id = a.org_id AND m.status = 'active'
		LEFT JOIN app.users u ON u.id = m.user_id
		WHERE a.id = $1::uuid
		ORDER BY m.created_at ASC
	`, nil, appID)
	if err != nil {
		return nil, fmt.Errorf("list app members: %w", err)
	}
	defer rows.Close()

	members := []OrgMember{}
	for rows.Next() {
		member, err := scanOrgMember(rows)
		if err != nil {
			return nil, fmt.Errorf("scan app member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate app members: %w", err)
	}
	return members, nil
}

func (r *PostgresRepository) RemoveMember(ctx context.Context, orgID, userID string) error {
	tag, err := r.db.ExecEx(ctx, `DELETE FROM app.org_members WHERE org_id = $1::uuid AND user_id = $2::uuid`, nil, orgID, userID)
	if err != nil {
		return fmt.Errorf("remove org member: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotOrgMember
	}
	return nil
}

func (r *PostgresRepository) CountOwners(ctx context.Context, orgID string) (int64, error) {
	var count int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.org_members WHERE org_id = $1::uuid AND role = 'owner' AND status = 'active'`, nil, orgID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count org owners: %w", err)
	}
	return count, nil
}

func (r *PostgresRepository) CountActiveMembers(ctx context.Context, orgID string) (int64, error) {
	var count int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.org_members WHERE org_id = $1::uuid AND status = 'active'`, nil, orgID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count org members: %w", err)
	}
	return count, nil
}

func (r *PostgresRepository) CountActiveOrgsForUser(ctx context.Context, userID string) (int64, error) {
	var count int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.org_members WHERE user_id = $1::uuid AND status = 'active'`, nil, userID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count user orgs: %w", err)
	}
	return count, nil
}

func (r *PostgresRepository) FindUserIDByEmail(ctx context.Context, email string) (string, error) {
	const query = `SELECT id::text FROM app.users WHERE lower(email) = lower($1) LIMIT 1`
	var id string
	err := r.db.QueryRowEx(ctx, query, nil, email).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrUserNotFound
	}
	if err != nil {
		return "", fmt.Errorf("find user by email: %w", err)
	}
	return id, nil
}

func scanKYCSubmission(row interface{ Scan(dest ...interface{}) error }) (KYCSubmission, error) {
	var sub KYCSubmission
	var reviewedBy, rejectionReason sql.NullString
	var reviewedAt sql.NullTime
	var fullName, idType, idNumber sql.NullString
	var dob, docBackURL, selfieURL sql.NullString
	if err := row.Scan(
		&sub.OrgID, &sub.BusinessName, &sub.TIN, &sub.IDDocumentURL,
		&sub.SubmittedAt, &reviewedBy, &reviewedAt, &rejectionReason,
		&fullName, &idType, &idNumber, &dob, &docBackURL, &selfieURL,
	); err != nil {
		return KYCSubmission{}, err
	}
	sub.ReviewedBy = reviewedBy.String
	if reviewedAt.Valid {
		sub.ReviewedAt = &reviewedAt.Time
	}
	sub.RejectionReason = rejectionReason.String
	sub.FullName = fullName.String
	sub.IDType = idType.String
	sub.IDNumber = idNumber.String
	sub.Dob = dob.String
	sub.IDDocumentBackURL = docBackURL.String
	sub.SelfieURL = selfieURL.String
	return sub, nil
}

const kycSubmissionSelect = `
	SELECT org_id::text, business_name, tin, id_document_url, submitted_at,
	       reviewed_by, reviewed_at, COALESCE(rejection_reason, ''),
	       COALESCE(full_name, ''), COALESCE(id_type, ''), COALESCE(id_number, ''),
	       COALESCE(dob::text, ''), COALESCE(id_document_back_url, ''), COALESCE(selfie_url, '')
	FROM app.kyc_submissions
`

// SubmitKYC records (or resubmits) verification evidence and flips the org
// to submitted. Resubmission clears any prior review outcome.
func (r *PostgresRepository) SubmitKYC(ctx context.Context, orgID, businessName, tin, docURL string) (KYCSubmission, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return KYCSubmission{}, fmt.Errorf("begin submit kyc transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url, full_name, id_type, id_number)
		VALUES ($1::uuid, $2, $3, $4, '', '', '')
		ON CONFLICT (org_id) DO UPDATE SET
			business_name = EXCLUDED.business_name,
			tin = EXCLUDED.tin,
			id_document_url = EXCLUDED.id_document_url,
			full_name = '',
			id_type = '',
			id_number = '',
			dob = NULL,
			id_document_back_url = '',
			selfie_url = '',
			submitted_at = NOW(),
			reviewed_by = NULL,
			reviewed_at = NULL,
			rejection_reason = ''
	`, nil, orgID, businessName, tin, docURL); err != nil {
		return KYCSubmission{}, fmt.Errorf("upsert kyc submission: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `
		UPDATE app.organizations SET business_name = $2, tin = $3, kyc_status = 'submitted', updated_at = NOW()
		WHERE id = $1::uuid
	`, nil, orgID, valueOrNil(businessName), valueOrNil(tin)); err != nil {
		return KYCSubmission{}, fmt.Errorf("mark org kyc submitted: %w", err)
	}
	// History: every (re)submission appends an immutable attempt row.
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submission_attempts (org_id, business_name, tin, id_document_url, status, full_name, id_type, id_number)
		VALUES ($1::uuid, $2, $3, $4, 'submitted', '', '', '')
	`, nil, orgID, businessName, tin, docURL); err != nil {
		return KYCSubmission{}, fmt.Errorf("record kyc attempt: %w", err)
	}
	var sub KYCSubmission
	sub, err = scanKYCSubmission(tx.QueryRowEx(ctx, kycSubmissionSelect+` WHERE org_id = $1::uuid`, nil, orgID))
	if err != nil {
		return KYCSubmission{}, fmt.Errorf("load kyc submission: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return KYCSubmission{}, fmt.Errorf("commit submit kyc transaction: %w", err)
	}
	return sub, nil
}

// GetKYCSubmission loads the evidence row; found=false when never submitted.
func (r *PostgresRepository) GetKYCSubmission(ctx context.Context, orgID string) (KYCSubmission, bool, error) {
	sub, err := scanKYCSubmission(r.db.QueryRowEx(ctx, kycSubmissionSelect+` WHERE org_id = $1::uuid`, nil, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return KYCSubmission{}, false, nil
	}
	if err != nil {
		return KYCSubmission{}, false, fmt.Errorf("get kyc submission: %w", err)
	}
	return sub, true, nil
}

// ReviewKYC records an admin decision. Only orgs sitting in submitted
// with a live submission row are reviewable — approving thin air (or
// re-deciding a closed file) is refused so the queue can't be bypassed.
func (r *PostgresRepository) ReviewKYC(ctx context.Context, orgID, status, reviewedBy, reason string) (Organization, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return Organization{}, fmt.Errorf("begin review kyc transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	var current string
	err = tx.QueryRowEx(ctx, `SELECT kyc_status FROM app.organizations WHERE id = $1::uuid`, nil, orgID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("load org kyc status: %w", err)
	}
	if current != "submitted" {
		return Organization{}, ErrKYCNotInReview
	}
	var hasSubmission bool
	err = tx.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.kyc_submissions WHERE org_id = $1::uuid)`, nil, orgID).Scan(&hasSubmission)
	if err != nil {
		return Organization{}, fmt.Errorf("check kyc submission: %w", err)
	}
	if !hasSubmission {
		return Organization{}, ErrKYCNotInReview
	}

	if _, err = tx.ExecEx(ctx, `
		UPDATE app.kyc_submissions
		SET reviewed_by = $2, reviewed_at = NOW(), rejection_reason = $3
		WHERE org_id = $1::uuid
	`, nil, orgID, valueOrNil(reviewedBy), reason); err != nil {
		return Organization{}, fmt.Errorf("record kyc review: %w", err)
	}
	var org Organization
	org, err = scanOrganization(tx.QueryRowEx(ctx, `
		UPDATE app.organizations SET kyc_status = $2, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID, status))
	if err != nil {
		return Organization{}, fmt.Errorf("mark org kyc reviewed: %w", err)
	}
	// History: every decision appends an immutable attempt row.
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submission_attempts
		  (org_id, business_name, tin, id_document_url, status, rejection_reason, reviewed_by, reviewed_at, full_name, id_type, id_number, dob, id_document_back_url, selfie_url)
		SELECT org_id, business_name, tin, id_document_url, $2, $3, $4, NOW(), full_name, id_type, id_number, dob, id_document_back_url, selfie_url
		FROM app.kyc_submissions WHERE org_id = $1::uuid
	`, nil, orgID, status, reason, valueOrNil(reviewedBy)); err != nil {
		return Organization{}, fmt.Errorf("record kyc attempt: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit review kyc transaction: %w", err)
	}
	return org, nil
}

// ListKYCQueue returns orgs holding submissions, newest first. Empty
// status selects submitted (the actionable queue).
func (r *PostgresRepository) ListKYCQueue(ctx context.Context, status string) ([]KYCQueueItem, error) {
	status = strings.TrimSpace(status)
	args := []any{}
	condition := ""
	if status == "" {
		status = "submitted"
	}
	if status != "all" {
		condition = "WHERE o.kyc_status = $1"
		args = append(args, status)
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.name, o.slug, o.kyc_status,
		       COALESCE(o.account_kind, 'merchant'),
		       COALESCE(s.business_name, ''), COALESCE(s.tin, ''),
		       COALESCE(s.full_name, ''), COALESCE(s.id_type, ''),
		       COALESCE(s.id_number, ''),
		       COALESCE(sv.category, ''),
		       COALESCE(sv.expected_volume_band, ''), COALESCE(sv.expected_txn_band, ''),
		       COALESCE(s.id_document_url, '') <> '',
		       COALESCE(s.id_document_back_url, '') <> '',
		       COALESCE(s.selfie_url, '') <> '',
		       COALESCE(s.dob::text, ''),
		       s.submitted_at, COALESCE(s.rejection_reason, ''),
		       COALESCE((SELECT u.email FROM app.org_members m JOIN app.users u ON u.id = m.user_id
		                 WHERE m.org_id = o.id AND m.role = 'owner' AND m.status = 'active'
		                 ORDER BY m.created_at ASC LIMIT 1), ''),
		       COALESCE((SELECT u.full_name FROM app.org_members m JOIN app.users u ON u.id = m.user_id
		                 WHERE m.org_id = o.id AND m.role = 'owner' AND m.status = 'active'
		                 ORDER BY m.created_at ASC LIMIT 1), ''),
		       COALESCE((SELECT u.phone FROM app.org_members m JOIN app.users u ON u.id = m.user_id
		                 WHERE m.org_id = o.id AND m.role = 'owner' AND m.status = 'active'
		                 ORDER BY m.created_at ASC LIMIT 1), '')
		FROM app.organizations o
		JOIN app.kyc_submissions s ON s.org_id = o.id
		LEFT JOIN app.creator_onboarding_surveys sv ON sv.org_id = o.id
		%s
		ORDER BY s.submitted_at DESC
	`, condition), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("list kyc queue: %w", err)
	}
	defer rows.Close()

	items := []KYCQueueItem{}
	for rows.Next() {
		var item KYCQueueItem
		if err := rows.Scan(
			&item.OrgID, &item.OrgName, &item.Slug, &item.KYCStatus,
			&item.AccountKind, &item.BusinessName, &item.TIN,
			&item.FullName, &item.IDType, &item.IDNumber,
			&item.Category,
			&item.ExpectedVolumeBand, &item.ExpectedTxnBand,
			&item.HasDocument, &item.HasBackDocument, &item.HasSelfie, &item.Dob,
			&item.SubmittedAt, &item.RejectionReason,
			&item.OwnerEmail, &item.OwnerName, &item.OwnerPhone,
		); err != nil {
			return nil, fmt.Errorf("scan kyc queue item: %w", err)
		}
		item.SuggestedRiskTier = SuggestedCreatorRiskTier(item.ExpectedVolumeBand, item.ExpectedTxnBand)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate kyc queue: %w", err)
	}
	return items, nil
}

// PlatformStats is the admin home dashboard snapshot: tenant counts,
// verification funnel, and withdrawal workload. One call, plain COUNTs.
type PlatformStats struct {
	Customers           int64            `json:"customers"`
	Admins              int64            `json:"admins"`
	Organizations       int64            `json:"organizations"`
	OrgsByKYC           map[string]int64 `json:"orgs_by_kyc"`
	KYCAwaitingReview   int64            `json:"kyc_awaiting_review"`
	Apps                int64            `json:"apps"`
	WithdrawalsByStatus map[string]int64 `json:"withdrawals_by_status"`
}

// PlatformStats counts tenants and workload for the admin dashboard.
// No actor check — the RequireAdminAuth route gate replaces membership.
func (r *PostgresRepository) PlatformStats(ctx context.Context) (PlatformStats, error) {
	stats := PlatformStats{
		OrgsByKYC:           map[string]int64{},
		WithdrawalsByStatus: map[string]int64{},
	}
	count := func(query string) (int64, error) {
		var n int64
		if err := r.db.QueryRowEx(ctx, query, nil).Scan(&n); err != nil {
			return 0, err
		}
		return n, nil
	}
	var err error
	if stats.Customers, err = count(`SELECT COUNT(*) FROM app.users`); err != nil {
		return PlatformStats{}, fmt.Errorf("count customers: %w", err)
	}
	if stats.Admins, err = count(`SELECT COUNT(*) FROM app.admin_users`); err != nil {
		return PlatformStats{}, fmt.Errorf("count admins: %w", err)
	}
	if stats.Organizations, err = count(`SELECT COUNT(*) FROM app.organizations`); err != nil {
		return PlatformStats{}, fmt.Errorf("count organizations: %w", err)
	}
	if stats.Apps, err = count(`SELECT COUNT(*) FROM app.payment_apps WHERE status != 'deleted'`); err != nil {
		return PlatformStats{}, fmt.Errorf("count apps: %w", err)
	}
	rows, err := r.db.QueryEx(ctx, `SELECT kyc_status, COUNT(*) FROM app.organizations GROUP BY kyc_status`, nil)
	if err != nil {
		return PlatformStats{}, fmt.Errorf("count orgs by kyc: %w", err)
	}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return PlatformStats{}, fmt.Errorf("scan orgs by kyc: %w", err)
		}
		stats.OrgsByKYC[status] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PlatformStats{}, fmt.Errorf("iterate orgs by kyc: %w", err)
	}
	stats.KYCAwaitingReview = stats.OrgsByKYC["submitted"]
	rows, err = r.db.QueryEx(ctx, `SELECT status, COUNT(*) FROM app.payment_withdrawals GROUP BY status`, nil)
	if err != nil {
		return PlatformStats{}, fmt.Errorf("count withdrawals by status: %w", err)
	}
	for rows.Next() {
		var status string
		var n int64
		if err := rows.Scan(&status, &n); err != nil {
			rows.Close()
			return PlatformStats{}, fmt.Errorf("scan withdrawals by status: %w", err)
		}
		stats.WithdrawalsByStatus[status] = n
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return PlatformStats{}, fmt.Errorf("iterate withdrawals by status: %w", err)
	}
	return stats, nil
}

// ListKYCAttempts returns the immutable submit/decide history, newest
// first. Empty (not an error) when nothing was ever submitted.
func (r *PostgresRepository) ListKYCAttempts(ctx context.Context, orgID string) ([]KYCAttempt, error) {
	rows, err := r.db.QueryEx(ctx, `
		SELECT id::text, org_id::text, business_name, tin, id_document_url,
		       status, COALESCE(rejection_reason, ''), reviewed_by, reviewed_at, created_at,
		       COALESCE(full_name, ''), COALESCE(id_type, ''), COALESCE(id_number, ''),
		       COALESCE(dob::text, ''), COALESCE(id_document_back_url, ''), COALESCE(selfie_url, '')
		FROM app.kyc_submission_attempts
		WHERE org_id = $1::uuid
		ORDER BY created_at DESC
	`, nil, orgID)
	if err != nil {
		return nil, fmt.Errorf("list kyc attempts: %w", err)
	}
	defer rows.Close()

	attempts := []KYCAttempt{}
	for rows.Next() {
		var a KYCAttempt
		var reviewedBy sql.NullString
		var reviewedAt sql.NullTime
		if err := rows.Scan(
			&a.ID, &a.OrgID, &a.BusinessName, &a.TIN, &a.IDDocumentURL,
			&a.Status, &a.RejectionReason, &reviewedBy, &reviewedAt, &a.CreatedAt,
			&a.FullName, &a.IDType, &a.IDNumber,
			&a.Dob, &a.IDDocumentBackURL, &a.SelfieURL,
		); err != nil {
			return nil, fmt.Errorf("scan kyc attempt: %w", err)
		}
		a.ReviewedBy = reviewedBy.String
		if reviewedAt.Valid {
			a.ReviewedAt = &reviewedAt.Time
		}
		attempts = append(attempts, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate kyc attempts: %w", err)
	}
	return attempts, nil
}

// GetNotificationPrefs returns the org's toggles; a never-saved org reads
// as all-enabled.
func (r *PostgresRepository) GetNotificationPrefs(ctx context.Context, orgID string) (NotificationPrefs, error) {
	var prefs NotificationPrefs
	err := r.db.QueryRowEx(ctx, `
		SELECT org_id::text, payment_updated, payment_refunded, payment_expired,
		       withdrawal_updates, kyc_decisions
		FROM app.notification_prefs WHERE org_id = $1::uuid
	`, nil, orgID).Scan(
		&prefs.OrgID, &prefs.PaymentUpdated, &prefs.PaymentRefunded,
		&prefs.PaymentExpired, &prefs.WithdrawalUpdates, &prefs.KYCDecisions,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return NotificationPrefs{
			OrgID: orgID, PaymentUpdated: true, PaymentRefunded: true,
			PaymentExpired: true, WithdrawalUpdates: true, KYCDecisions: true,
		}, nil
	}
	if err != nil {
		return NotificationPrefs{}, fmt.Errorf("get notification prefs: %w", err)
	}
	return prefs, nil
}

// UpsertNotificationPrefs replaces the org's toggles.
func (r *PostgresRepository) UpsertNotificationPrefs(ctx context.Context, prefs NotificationPrefs) (NotificationPrefs, error) {
	var out NotificationPrefs
	err := r.db.QueryRowEx(ctx, `
		INSERT INTO app.notification_prefs
		  (org_id, payment_updated, payment_refunded, payment_expired, withdrawal_updates, kyc_decisions, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5, $6, NOW())
		ON CONFLICT (org_id) DO UPDATE SET
		  payment_updated = EXCLUDED.payment_updated,
		  payment_refunded = EXCLUDED.payment_refunded,
		  payment_expired = EXCLUDED.payment_expired,
		  withdrawal_updates = EXCLUDED.withdrawal_updates,
		  kyc_decisions = EXCLUDED.kyc_decisions,
		  updated_at = NOW()
		RETURNING org_id::text, payment_updated, payment_refunded, payment_expired,
		          withdrawal_updates, kyc_decisions
	`, nil, prefs.OrgID, prefs.PaymentUpdated, prefs.PaymentRefunded,
		prefs.PaymentExpired, prefs.WithdrawalUpdates, prefs.KYCDecisions).Scan(
		&out.OrgID, &out.PaymentUpdated, &out.PaymentRefunded,
		&out.PaymentExpired, &out.WithdrawalUpdates, &out.KYCDecisions,
	)
	if err != nil {
		return NotificationPrefs{}, fmt.Errorf("upsert notification prefs: %w", err)
	}
	// Keep the normalized multi-channel table in sync with the legacy
	// compatibility endpoint. Critical in-app alerts remain enabled even if
	// an old client attempts to mute them.
	legacy := []struct {
		event   string
		enabled bool
	}{
		{"payment.succeeded", out.PaymentUpdated},
		{"payment.failed", out.PaymentUpdated},
		{"payment.refunded", out.PaymentRefunded},
		{"payment.expired", out.PaymentExpired},
		{"withdrawal.requested", out.WithdrawalUpdates},
		{"withdrawal.requires_approval", out.WithdrawalUpdates},
		{"withdrawal.approved", out.WithdrawalUpdates},
		{"withdrawal.rejected", out.WithdrawalUpdates},
		{"withdrawal.dispatched", out.WithdrawalUpdates},
		{"withdrawal.completed", out.WithdrawalUpdates},
		{"withdrawal.failed", out.WithdrawalUpdates},
		{"kyc.verified", out.KYCDecisions},
		{"kyc.rejected", out.KYCDecisions},
	}
	for _, item := range legacy {
		for _, channel := range []string{"in_app", "email", "sms"} {
			enabled := item.enabled
			if channel == "in_app" && (item.event == "payment.failed" || item.event == "withdrawal.completed" || item.event == "kyc.verified") {
				enabled = true
			}
			if _, syncErr := r.db.ExecEx(ctx, `INSERT INTO app.notification_preferences (scope_kind, scope_id, event_type, channel, enabled) VALUES ('org', $1::uuid, $2, $3, $4) ON CONFLICT (scope_kind, scope_id, event_type, channel) DO UPDATE SET enabled = EXCLUDED.enabled, updated_at = NOW()`, nil, out.OrgID, item.event, channel, enabled); syncErr != nil {
				return NotificationPrefs{}, fmt.Errorf("sync notification prefs: %w", syncErr)
			}
		}
	}
	return out, nil
}

// UpdateOrgLogo sets the org's logo location (uploaded private path or an
// external URL). Served only through the authenticated logo endpoint.
func (r *PostgresRepository) UpdateOrgLogo(ctx context.Context, orgID, logoURL string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations SET logo_url = NULLIF($2, ''), updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID, strings.TrimSpace(logoURL)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("update org logo: %w", err)
	}
	return org, nil
}

// SuspendOrg freezes an org's live money movement (reason required).
func (r *PostgresRepository) SuspendOrg(ctx context.Context, orgID, reason string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations
		SET suspended = true, suspended_reason = $2, suspended_at = NOW(), updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID, strings.TrimSpace(reason)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("suspend org: %w", err)
	}
	return org, nil
}

// UnsuspendOrg lifts a suspension.
func (r *PostgresRepository) UnsuspendOrg(ctx context.Context, orgID string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations
		SET suspended = false, suspended_reason = '', suspended_at = NULL, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("unsuspend org: %w", err)
	}
	return org, nil
}

// AuditEntry is one admin-action trail row.
type AuditEntry struct {
	ID        string `json:"id"`
	ActorID   string `json:"actor_id"`
	ActorEmail string `json:"actor_email"`
	Action    string `json:"action"`
	TargetType string `json:"target_type"`
	TargetID  string `json:"target_id"`
	Before    string `json:"before,omitempty"`
	After     string `json:"after,omitempty"`
	IP        string `json:"ip,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// WriteAudit records an admin mutation. Best-effort by convention:
// callers log but never fail the mutation on audit errors.
func (r *PostgresRepository) WriteAudit(ctx context.Context, actorID, actorEmail, action, targetType, targetID, ip string, before, after map[string]any) error {
	var beforeJSON, afterJSON *string
	if before != nil {
		if raw, err := json.Marshal(before); err == nil {
			str := string(raw)
			beforeJSON = &str
		}
	}
	if after != nil {
		if raw, err := json.Marshal(after); err == nil {
			str := string(raw)
			afterJSON = &str
		}
	}
	_, err := r.db.ExecEx(ctx, `INSERT INTO app.audit_log (actor_id, actor_email, action, target_type, target_id, before_data, after_data, ip)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7::jsonb, $8)`,
		nil, actorID, actorEmail, action, targetType, targetID, beforeJSON, afterJSON, ip)
	if err != nil {
		return fmt.Errorf("write audit log: %w", err)
	}
	return nil
}

// ListAudit returns the trail newest-first (admin viewer), paginated.
func (r *PostgresRepository) ListAudit(ctx context.Context, action, actor string, limit, offset int) ([]AuditEntry, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	conds := []string{}
	args := []any{}
	if action != "" {
		args = append(args, action)
		conds = append(conds, fmt.Sprintf("action = $%d", len(args)))
	}
	if actor != "" {
		args = append(args, "%"+actor+"%")
		conds = append(conds, fmt.Sprintf("actor_email ILIKE $%d", len(args)))
	}
	where := "TRUE"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT id::text, actor_id, actor_email, action, target_type, target_id,
		       COALESCE(before_data::text, ''), COALESCE(after_data::text, ''), COALESCE(ip, ''),
		       created_at
		FROM app.audit_log WHERE %s ORDER BY created_at DESC LIMIT %d OFFSET %d`, where, limit, offset), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("list audit log: %w", err)
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.ActorID, &e.ActorEmail, &e.Action, &e.TargetType, &e.TargetID,
			&e.Before, &e.After, &e.IP, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// OrgSuspensionByApp resolves an app to its org's suspension state for
// money-movement gates. Unknown apps error (fail closed).
func (r *PostgresRepository) OrgSuspensionByApp(ctx context.Context, appID string) (bool, string, error) {
	var suspended bool
	var reason sql.NullString
	err := r.db.QueryRowEx(ctx, `
		SELECT o.suspended, o.suspended_reason
		FROM app.payment_apps a
		JOIN app.organizations o ON o.id = a.org_id
		WHERE a.id = $1::uuid`, nil, appID).Scan(&suspended, &reason)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, "", ErrOrgNotFound
	}
	if err != nil {
		return false, "", fmt.Errorf("org suspension by app: %w", err)
	}
	return suspended, reason.String, nil
}

// UpdateOrgLiveLimits sets per-org live caps; empty clears to platform
// default (stored NULL, not zero — zero would forbid all live volume).
func (r *PostgresRepository) UpdateOrgLiveLimits(ctx context.Context, orgID, maxTxn, dailyCap string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations
		SET live_max_txn_amount = NULLIF($2, ''),
		    live_daily_volume_cap = NULLIF($3, ''),
		    updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID, strings.TrimSpace(maxTxn), strings.TrimSpace(dailyCap)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("update org live limits: %w", err)
	}
	return org, nil
}

// CreateCreatorOrganization inserts a creator-kind org with its public
// profile fields plus the single owner member. Handle collisions surface
// as ErrHandleTaken (unique index on lower(handle)).
func (r *PostgresRepository) CreateCreatorOrganization(ctx context.Context, name, slug string, in CreatorOrgInput, ownerUserID string) (Organization, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return Organization{}, fmt.Errorf("begin create creator transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	var org Organization
	org, err = scanOrganization(tx.QueryRowEx(ctx, `
		INSERT INTO app.organizations (name, slug, account_kind, display_name, handle, bio)
		VALUES ($1, $2, 'creator', $3, NULLIF($4, ''), $5)
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, name, slug, valueOrNil(in.DisplayName), strings.TrimSpace(in.Handle), valueOrNil(in.Bio)))
	if err != nil {
		if pgErr, ok := err.(pgx.PgError); ok && pgErr.Code == "23505" {
			return Organization{}, ErrHandleTaken
		}
		return Organization{}, fmt.Errorf("insert creator organization: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.org_members (org_id, user_id, role, invited_by, status)
		VALUES ($1::uuid, $2::uuid, 'owner', $2, 'active')
	`, nil, org.ID, ownerUserID); err != nil {
		return Organization{}, fmt.Errorf("insert owner membership: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit create creator transaction: %w", err)
	}
	return org, nil
}

// SubmitCreatorKYC files individual verification evidence and flips the
// creator org to submitted. Merchant columns are cleared so a creator file
// never carries a stale business identity.
func (r *PostgresRepository) SubmitCreatorKYC(ctx context.Context, orgID string, in CreatorKYCInput) (KYCSubmission, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return KYCSubmission{}, fmt.Errorf("begin submit creator kyc transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url, full_name, id_type, id_number, dob, id_document_back_url, selfie_url)
		VALUES ($1::uuid, '', '', $2, $3, $4, $5, NULLIF($6, '')::date, $7, $8)
		ON CONFLICT (org_id) DO UPDATE SET
			business_name = '',
			tin = '',
			id_document_url = EXCLUDED.id_document_url,
			full_name = EXCLUDED.full_name,
			id_type = EXCLUDED.id_type,
			id_number = EXCLUDED.id_number,
			dob = EXCLUDED.dob,
			id_document_back_url = EXCLUDED.id_document_back_url,
			selfie_url = EXCLUDED.selfie_url,
			submitted_at = NOW(),
			reviewed_by = NULL,
			reviewed_at = NULL,
			rejection_reason = ''
	`, nil, orgID, in.DocURL, in.FullName, in.IDType, in.IDNumber, in.Dob, in.DocBackURL, in.SelfieURL); err != nil {
		return KYCSubmission{}, fmt.Errorf("upsert creator kyc: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `
		UPDATE app.organizations SET kyc_status = 'submitted', updated_at = NOW()
		WHERE id = $1::uuid
	`, nil, orgID); err != nil {
		return KYCSubmission{}, fmt.Errorf("mark creator kyc submitted: %w", err)
	}
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submission_attempts (org_id, business_name, tin, id_document_url, status, full_name, id_type, id_number, dob, id_document_back_url, selfie_url)
		VALUES ($1::uuid, '', '', $2, 'submitted', $3, $4, $5, NULLIF($6, '')::date, $7, $8)
	`, nil, orgID, in.DocURL, in.FullName, in.IDType, in.IDNumber, in.Dob, in.DocBackURL, in.SelfieURL); err != nil {
		return KYCSubmission{}, fmt.Errorf("record creator kyc attempt: %w", err)
	}
	var sub KYCSubmission
	sub, err = scanKYCSubmission(tx.QueryRowEx(ctx, kycSubmissionSelect+` WHERE org_id = $1::uuid`, nil, orgID))
	if err != nil {
		return KYCSubmission{}, fmt.Errorf("load creator kyc: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return KYCSubmission{}, fmt.Errorf("commit creator kyc transaction: %w", err)
	}
	return sub, nil
}

// GetOrganizationByHandle resolves a public creator handle
// (case-insensitive). Non-creator rows are invisible here — the support
// page namespace belongs to creators only.
func (r *PostgresRepository) GetOrganizationByHandle(ctx context.Context, handle string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, organizationSelect+` WHERE lower(handle) = lower($1) AND account_kind = 'creator'`, nil, strings.TrimSpace(handle)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("get organization by handle: %w", err)
	}
	return org, nil
}

func scanCreatorSurvey(row interface{ Scan(dest ...interface{}) error }) (CreatorSurvey, error) {
	var s CreatorSurvey
	var categoryOther, referralSource, useCasesRaw sql.NullString
	if err := row.Scan(
		&s.OrgID, &s.Category, &categoryOther, &referralSource, &useCasesRaw,
		&s.ExpectedVolumeBand, &s.ExpectedTxnBand, &s.CreatedAt, &s.UpdatedAt,
	); err != nil {
		return CreatorSurvey{}, err
	}
	s.CategoryOther = categoryOther.String
	s.ReferralSource = referralSource.String
	s.UseCases = []string{}
	if useCasesRaw.Valid && strings.TrimSpace(useCasesRaw.String) != "" {
		var decoded []string
		if err := json.Unmarshal([]byte(useCasesRaw.String), &decoded); err == nil && decoded != nil {
			s.UseCases = decoded
		}
	}
	s.SuggestedRiskTier = SuggestedCreatorRiskTier(s.ExpectedVolumeBand, s.ExpectedTxnBand)
	return s, nil
}

// UpsertCreatorSurvey stores (or replaces) a creator org's onboarding
// answers. Display name is applied to the org row in the same
// transaction so the survey form submits once.
func (r *PostgresRepository) UpsertCreatorSurvey(ctx context.Context, orgID string, in CreatorSurveyInput) (CreatorSurvey, error) {
	useCasesJSON, err := json.Marshal(in.UseCases)
	if err != nil {
		return CreatorSurvey{}, fmt.Errorf("encode survey use cases: %w", err)
	}
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return CreatorSurvey{}, fmt.Errorf("begin upsert survey transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	if strings.TrimSpace(in.DisplayName) != "" {
		if _, err = tx.ExecEx(ctx, `
			UPDATE app.organizations SET display_name = $2, updated_at = NOW()
			WHERE id = $1::uuid AND account_kind = 'creator'
		`, nil, orgID, strings.TrimSpace(in.DisplayName)); err != nil {
			return CreatorSurvey{}, fmt.Errorf("apply survey display name: %w", err)
		}
	}
	var survey CreatorSurvey
	survey, err = scanCreatorSurvey(tx.QueryRowEx(ctx, `
		INSERT INTO app.creator_onboarding_surveys
		  (org_id, category, category_other, referral_source, use_cases, expected_volume_band, expected_txn_band, updated_at)
		VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7, NOW())
		ON CONFLICT (org_id) DO UPDATE SET
		  category = EXCLUDED.category,
		  category_other = EXCLUDED.category_other,
		  referral_source = EXCLUDED.referral_source,
		  use_cases = EXCLUDED.use_cases,
		  expected_volume_band = EXCLUDED.expected_volume_band,
		  expected_txn_band = EXCLUDED.expected_txn_band,
		  updated_at = NOW()
		RETURNING org_id::text, category, category_other, referral_source, use_cases::text,
		          expected_volume_band, expected_txn_band, created_at, updated_at
	`, nil, orgID, in.Category, in.CategoryOther, in.ReferralSource, string(useCasesJSON), in.ExpectedVolumeBand, in.ExpectedTxnBand))
	if err != nil {
		return CreatorSurvey{}, fmt.Errorf("upsert creator survey: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return CreatorSurvey{}, fmt.Errorf("commit upsert survey transaction: %w", err)
	}
	return survey, nil
}

// GetCreatorSurvey loads a creator org's onboarding answers;
// found=false when the survey was never submitted.
func (r *PostgresRepository) GetCreatorSurvey(ctx context.Context, orgID string) (CreatorSurvey, bool, error) {
	survey, err := scanCreatorSurvey(r.db.QueryRowEx(ctx, `
		SELECT org_id::text, category, category_other, referral_source, use_cases::text,
		       expected_volume_band, expected_txn_band, created_at, updated_at
		FROM app.creator_onboarding_surveys WHERE org_id = $1::uuid
	`, nil, orgID))
	if errors.Is(err, pgx.ErrNoRows) {
		return CreatorSurvey{}, false, nil
	}
	if err != nil {
		return CreatorSurvey{}, false, fmt.Errorf("get creator survey: %w", err)
	}
	return survey, true, nil
}

// SwitchCreatorToMerchant converts a pre-KYC creator org to the merchant
// track: kind flips, creator profile/survey rows are cleared. Refused once
// any KYC submission exists (kind is support-assisted after that point).
func (r *PostgresRepository) SwitchCreatorToMerchant(ctx context.Context, orgID string) (Organization, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return Organization{}, fmt.Errorf("begin kind switch transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	var kind, kycStatus string
	err = tx.QueryRowEx(ctx, `SELECT account_kind, kyc_status FROM app.organizations WHERE id = $1::uuid`, nil, orgID).Scan(&kind, &kycStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("load org kind: %w", err)
	}
	if kind != AccountKindCreator {
		return Organization{}, ErrAccountKindImmutable
	}
	var hasKYC bool
	err = tx.QueryRowEx(ctx, `SELECT EXISTS (SELECT 1 FROM app.kyc_submissions WHERE org_id = $1::uuid)`, nil, orgID).Scan(&hasKYC)
	if err != nil {
		return Organization{}, fmt.Errorf("check kyc submission: %w", err)
	}
	if hasKYC || kycStatus != "pending" {
		return Organization{}, ErrKindSwitchSubmitted
	}
	if _, err = tx.ExecEx(ctx, `DELETE FROM app.creator_onboarding_surveys WHERE org_id = $1::uuid`, nil, orgID); err != nil {
		return Organization{}, fmt.Errorf("clear creator survey: %w", err)
	}
	var org Organization
	org, err = scanOrganization(tx.QueryRowEx(ctx, `
		UPDATE app.organizations
		SET account_kind = 'merchant', display_name = NULL, handle = NULL, bio = NULL, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          suspended, COALESCE(suspended_reason, ''),
		          COALESCE(account_kind, 'merchant'), COALESCE(display_name, ''),
		          COALESCE(handle, ''), COALESCE(bio, ''),
		          created_at, updated_at
	`, nil, orgID))
	if err != nil {
		return Organization{}, fmt.Errorf("switch org kind: %w", err)
	}
	if err = tx.CommitEx(ctx); err != nil {
		return Organization{}, fmt.Errorf("commit kind switch transaction: %w", err)
	}
	return org, nil
}

func scanSupportLink(row interface{ Scan(dest ...interface{}) error }) (SupportLink, error) {
	var l SupportLink
	var amount sql.NullString
	if err := row.Scan(
		&l.ID, &l.OrgID, &l.Label, &l.AmountMode, &amount,
		&l.SortOrder, &l.Active, &l.CreatedAt,
	); err != nil {
		return SupportLink{}, err
	}
	l.Amount = amount.String
	return l, nil
}

const supportLinkSelect = `
	SELECT id::text, org_id::text, label, amount_mode, amount,
	       sort_order, active, created_at
	FROM app.creator_support_links
`

func (r *PostgresRepository) listSupportLinks(ctx context.Context, q interface {
	QueryEx(ctx context.Context, query string, options *pgx.QueryExOptions, args ...interface{}) (*pgx.Rows, error)
}, orgID string) ([]SupportLink, error) {
	rows, err := q.QueryEx(ctx, supportLinkSelect+` WHERE org_id = $1::uuid ORDER BY sort_order ASC, created_at ASC`, nil, orgID)
	if err != nil {
		return nil, fmt.Errorf("list support links: %w", err)
	}
	defer rows.Close()

	links := []SupportLink{}
	for rows.Next() {
		link, err := scanSupportLink(rows)
		if err != nil {
			return nil, fmt.Errorf("scan support link: %w", err)
		}
		links = append(links, link)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate support links: %w", err)
	}
	return links, nil
}

// GetSupportSettings loads a creator org's public-page configuration;
// found=false when the page was never enabled.
func (r *PostgresRepository) GetSupportSettings(ctx context.Context, orgID string) (SupportSettings, bool, error) {
	var s SupportSettings
	var appID, minAmount, maxAmount sql.NullString
	err := r.db.QueryRowEx(ctx, `
		SELECT org_id::text, support_app_id::text, min_amount, max_amount,
		       show_supporters_wall, created_at, updated_at
		FROM app.creator_support_settings WHERE org_id = $1::uuid
	`, nil, orgID).Scan(
		&s.OrgID, &appID, &minAmount, &maxAmount,
		&s.ShowSupportersWall, &s.CreatedAt, &s.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return SupportSettings{}, false, nil
	}
	if err != nil {
		return SupportSettings{}, false, fmt.Errorf("get support settings: %w", err)
	}
	s.SupportAppID = appID.String
	s.MinAmount = minAmount.String
	s.MaxAmount = maxAmount.String
	links, err := r.listSupportLinks(ctx, r.db, orgID)
	if err != nil {
		return SupportSettings{}, false, err
	}
	s.Links = links
	return s, true, nil
}

// UpsertSupportSettings replaces the page configuration and its buttons
// in one transaction (links are delete-and-reinsert, display order from
// the request order).
func (r *PostgresRepository) UpsertSupportSettings(ctx context.Context, orgID string, in SupportSettingsInput) (SupportSettings, error) {
	tx, err := r.db.BeginEx(ctx, nil)
	if err != nil {
		return SupportSettings{}, fmt.Errorf("begin upsert support settings transaction: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.RollbackEx(ctx)
		}
	}()

	var s SupportSettings
	var appID, minAmount, maxAmount sql.NullString
	err = tx.QueryRowEx(ctx, `
		INSERT INTO app.creator_support_settings (org_id, support_app_id, min_amount, max_amount, updated_at)
		VALUES ($1::uuid, NULLIF($2, '')::uuid, NULLIF($3, ''), NULLIF($4, ''), NOW())
		ON CONFLICT (org_id) DO UPDATE SET
		  support_app_id = EXCLUDED.support_app_id,
		  min_amount = EXCLUDED.min_amount,
		  max_amount = EXCLUDED.max_amount,
		  updated_at = NOW()
		RETURNING org_id::text, support_app_id::text, min_amount, max_amount,
		          show_supporters_wall, created_at, updated_at
	`, nil, orgID, strings.TrimSpace(in.SupportAppID), strings.TrimSpace(in.MinAmount), strings.TrimSpace(in.MaxAmount)).Scan(
		&s.OrgID, &appID, &minAmount, &maxAmount,
		&s.ShowSupportersWall, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return SupportSettings{}, fmt.Errorf("upsert support settings: %w", err)
	}
	s.SupportAppID = appID.String
	s.MinAmount = minAmount.String
	s.MaxAmount = maxAmount.String

	if _, err = tx.ExecEx(ctx, `DELETE FROM app.creator_support_links WHERE org_id = $1::uuid`, nil, orgID); err != nil {
		return SupportSettings{}, fmt.Errorf("clear support links: %w", err)
	}
	links := []SupportLink{}
	for i, l := range in.Links {
		var link SupportLink
		var amount sql.NullString
		err = tx.QueryRowEx(ctx, `
			INSERT INTO app.creator_support_links (org_id, label, amount_mode, amount, sort_order, active)
			VALUES ($1::uuid, $2, $3, NULLIF($4, ''), $5, $6)
			RETURNING id::text, org_id::text, label, amount_mode, amount,
			          sort_order, active, created_at
		`, nil, orgID, strings.TrimSpace(l.Label), strings.TrimSpace(l.AmountMode),
			strings.TrimSpace(l.Amount), i, l.Active).Scan(
			&link.ID, &link.OrgID, &link.Label, &link.AmountMode, &amount,
			&link.SortOrder, &link.Active, &link.CreatedAt,
		)
		if err != nil {
			return SupportSettings{}, fmt.Errorf("insert support link: %w", err)
		}
		link.Amount = amount.String
		links = append(links, link)
	}
	s.Links = links
	if err = tx.CommitEx(ctx); err != nil {
		return SupportSettings{}, fmt.Errorf("commit upsert support settings transaction: %w", err)
	}
	return s, nil
}
