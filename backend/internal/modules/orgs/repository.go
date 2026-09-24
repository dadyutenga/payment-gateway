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
	UpdateOrganization(ctx context.Context, orgID, name, businessName string) (Organization, error)
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
	var businessName, tin sql.NullString
	if err := row.Scan(
		&org.ID, &org.Name, &org.Slug, &org.KYCStatus,
		&businessName, &tin, &org.CreatedAt, &org.UpdatedAt,
	); err != nil {
		return Organization{}, err
	}
	org.BusinessName = businessName.String
	org.TIN = tin.String
	return org, nil
}

const organizationSelect = `
	SELECT id::text, name, slug, kyc_status,
	       business_name, tin, created_at, updated_at
	FROM app.organizations
`

func scanOrgMember(row interface {
	Scan(dest ...interface{}) error
}) (OrgMember, error) {
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
		RETURNING id::text, name, slug, kyc_status, business_name, tin, created_at, updated_at
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
		       o.business_name, o.tin, o.created_at, o.updated_at,
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
		var businessName, tin sql.NullString
		var role, status string
		if err := rows.Scan(
			&item.ID, &item.Name, &item.Slug, &item.KYCStatus,
			&businessName, &tin, &item.CreatedAt, &item.UpdatedAt,
			&role, &status,
		); err != nil {
			return nil, fmt.Errorf("scan organization: %w", err)
		}
		item.BusinessName = businessName.String
		item.TIN = tin.String
		item.Role = Role(role)
		item.Status = MemberStatus(status)
		orgs = append(orgs, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate organizations: %w", err)
	}
	return orgs, nil
}

func (r *PostgresRepository) UpdateOrganization(ctx context.Context, orgID, name, businessName string) (Organization, error) {
	org, err := scanOrganization(r.db.QueryRowEx(ctx, `
		UPDATE app.organizations SET name = $2, business_name = $3, updated_at = NOW()
		WHERE id = $1::uuid
		RETURNING id::text, name, slug, kyc_status, business_name, tin, created_at, updated_at
	`, nil, orgID, name, valueOrNil(businessName)))
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
