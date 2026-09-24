package orgs

import (
	"context"
	"log/slog"
	"strings"

	"azsubay-payments-gateway/internal/shared/validation"
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

func (s *Service) UpdateOrganization(ctx context.Context, userID, orgID, name, businessName string) (Organization, validation.Errors, error) {
	if _, err := s.CheckOrgPermission(ctx, userID, orgID, PermManageOrg); err != nil {
		return Organization{}, nil, err
	}
	name = strings.TrimSpace(name)
	businessName = strings.TrimSpace(businessName)

	errs := validation.Errors{}
	validation.Required(name, "Name is required.", errs, "name")
	validation.MaxRunes(name, 100, "Name must be 100 characters or fewer.", errs, "name")
	validation.MaxRunes(businessName, 200, "Business name must be 200 characters or fewer.", errs, "business_name")
	if errs.Any() {
		return Organization{}, errs, nil
	}
	org, err := s.repo.UpdateOrganization(ctx, strings.TrimSpace(orgID), name, businessName)
	return org, nil, err
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
	return s.repo.InviteMember(ctx, strings.TrimSpace(orgID), userID, role, actorUserID)
}

func (s *Service) AcceptInvite(ctx context.Context, userID, orgID string) (OrgMember, error) {
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
