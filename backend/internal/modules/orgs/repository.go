package orgs

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

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
	// UpsertNotificationPrefs replaces the org's toggles.
	UpsertNotificationPrefs(ctx context.Context, prefs NotificationPrefs) (NotificationPrefs, error)
	// PlatformStats counts tenants and workload for the admin dashboard.
	PlatformStats(ctx context.Context) (PlatformStats, error)
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
	if err := row.Scan(
		&org.ID, &org.Name, &org.Slug, &org.KYCStatus,
		&businessName, &tin, &maxTxn, &dailyCap,
		&address, &phone, &contactEmail, &logoURL, &primaryColor,
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
	return org, nil
}

const organizationSelect = `
	SELECT id::text, name, slug, kyc_status,
	       business_name, tin, live_max_txn_amount, live_daily_volume_cap,
	       address, phone, contact_email, logo_url, primary_color,
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
		var role, status string
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.KYCStatus,
			&businessName, &tin, &maxTxn, &dailyCap,
			&address, &phone, &contactEmail, &logoURL, &primaryColor,
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
		    contact_email = $7, logo_url = $8, primary_color = $9, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin,
		          live_max_txn_amount, live_daily_volume_cap,
		          address, phone, contact_email, logo_url, primary_color,
		          created_at, updated_at
	`, nil, orgID, upd.Name, valueOrNil(upd.BusinessName), valueOrNil(upd.TIN),
		valueOrNil(upd.Address), valueOrNil(upd.Phone), valueOrNil(upd.ContactEmail),
		valueOrNil(upd.LogoURL), valueOrNil(upd.PrimaryColor)))
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
	if err := row.Scan(
		&sub.OrgID, &sub.BusinessName, &sub.TIN, &sub.IDDocumentURL,
		&sub.SubmittedAt, &reviewedBy, &reviewedAt, &rejectionReason,
	); err != nil {
		return KYCSubmission{}, err
	}
	sub.ReviewedBy = reviewedBy.String
	if reviewedAt.Valid {
		sub.ReviewedAt = &reviewedAt.Time
	}
	sub.RejectionReason = rejectionReason.String
	return sub, nil
}

const kycSubmissionSelect = `
	SELECT org_id::text, business_name, tin, id_document_url, submitted_at,
	       reviewed_by, reviewed_at, COALESCE(rejection_reason, '')
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
		INSERT INTO app.kyc_submissions (org_id, business_name, tin, id_document_url)
		VALUES ($1::uuid, $2, $3, $4)
		ON CONFLICT (org_id) DO UPDATE SET
			business_name = EXCLUDED.business_name,
			tin = EXCLUDED.tin,
			id_document_url = EXCLUDED.id_document_url,
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
		INSERT INTO app.kyc_submission_attempts (org_id, business_name, tin, id_document_url, status)
		VALUES ($1::uuid, $2, $3, $4, 'submitted')
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
		          created_at, updated_at
	`, nil, orgID, status))
	if err != nil {
		return Organization{}, fmt.Errorf("mark org kyc reviewed: %w", err)
	}
	// History: every decision appends an immutable attempt row.
	if _, err = tx.ExecEx(ctx, `
		INSERT INTO app.kyc_submission_attempts
		  (org_id, business_name, tin, id_document_url, status, rejection_reason, reviewed_by, reviewed_at)
		SELECT org_id, business_name, tin, id_document_url, $2, $3, $4, NOW()
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
		       COALESCE(s.business_name, ''), COALESCE(s.tin, ''),
		       COALESCE(s.id_document_url, '') <> '',
		       s.submitted_at, COALESCE(s.rejection_reason, '')
		FROM app.organizations o
		JOIN app.kyc_submissions s ON s.org_id = o.id
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
			&item.BusinessName, &item.TIN, &item.HasDocument,
			&item.SubmittedAt, &item.RejectionReason,
		); err != nil {
			return nil, fmt.Errorf("scan kyc queue item: %w", err)
		}
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
		       status, COALESCE(rejection_reason, ''), reviewed_by, reviewed_at, created_at
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
		          live_max_txn_amount, live_daily_volume_cap, created_at, updated_at
	`, nil, orgID, strings.TrimSpace(maxTxn), strings.TrimSpace(dailyCap)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Organization{}, ErrOrgNotFound
	}
	if err != nil {
		return Organization{}, fmt.Errorf("update org live limits: %w", err)
	}
	return org, nil
}
