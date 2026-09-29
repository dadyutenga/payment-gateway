package orgs

import (
	"context"
	"testing"
)

func TestPermissionMatrix(t *testing.T) {
	cases := []struct {
		role Role
		perm Permission
		want bool
	}{
		{RoleOwner, PermRead, true},
		{RoleOwner, PermWithdraw, true},
		{RoleOwner, PermDevelop, true},
		{RoleOwner, PermManageMembers, true},
		{RoleOwner, PermManageOrg, true},
		{RoleOwner, PermDeleteOrg, true},
		{RoleFinance, PermRead, true},
		{RoleFinance, PermWithdraw, true},
		{RoleFinance, PermDevelop, false},
		{RoleFinance, PermManageMembers, false},
		{RoleFinance, PermManageOrg, false},
		{RoleFinance, PermDeleteOrg, false},
		{RoleDeveloper, PermRead, true},
		{RoleDeveloper, PermWithdraw, false},
		{RoleDeveloper, PermDevelop, true},
		{RoleDeveloper, PermManageMembers, false},
		{RoleDeveloper, PermManageOrg, false},
		{RoleDeveloper, PermDeleteOrg, false},
		{RoleViewer, PermRead, true},
		{RoleViewer, PermWithdraw, false},
		{RoleViewer, PermDevelop, false},
		{RoleViewer, PermManageMembers, false},
		{RoleViewer, PermManageOrg, false},
		{RoleViewer, PermDeleteOrg, false},
		{Role("superadmin"), PermRead, false},
	}
	for _, tc := range cases {
		if got := Can(tc.role, tc.perm); got != tc.want {
			t.Fatalf("Can(%q, %q) = %v, want %v", tc.role, tc.perm, got, tc.want)
		}
	}
}

type fakeOrgRepository struct {
	members      map[string]OrgMember // orgID + "\x00" + userID
	memberErr    error
	owners       int64
	apps         int64
	userIDs      map[string]string
	userErr      error
	createdOrgs  int
	removed      [][2]string
	updatedRoles [][3]string
	invited      []OrgMember
	org          Organization
	kycFound     bool
	reviewErr    error
	reviewedStatus string
	reviewedBy   string
	reviewReason string
	queue        []KYCQueueItem
	queueErr     error
	limitsMaxTxn string
	limitsDailyCap string
}

func orgKey(orgID, userID string) string { return orgID + "\x00" + userID }

func newFakeOrgRepository() *fakeOrgRepository {
	return &fakeOrgRepository{members: map[string]OrgMember{}, userIDs: map[string]string{}}
}

func (r *fakeOrgRepository) CreateOrganization(_ context.Context, name, slug, businessName, ownerUserID string) (Organization, error) {
	r.createdOrgs++
	return Organization{ID: "org_test", Name: name, Slug: slug, BusinessName: businessName, KYCStatus: "pending"}, nil
}

func (r *fakeOrgRepository) GetOrganization(_ context.Context, orgID string) (Organization, error) {
	if r.org.ID != "" {
		return r.org, nil
	}
	return Organization{ID: orgID, Name: "Test Org", KYCStatus: "pending"}, nil
}

func (r *fakeOrgRepository) ListOrganizationsForUser(_ context.Context, _ string) ([]OrganizationWithRole, error) {
	return nil, nil
}

func (r *fakeOrgRepository) UpdateOrganization(_ context.Context, orgID string, upd OrgProfileUpdate) (Organization, error) {
	return Organization{ID: orgID, Name: upd.Name, BusinessName: upd.BusinessName, TIN: upd.TIN}, nil
}

func (r *fakeOrgRepository) ListKYCAttempts(_ context.Context, _ string) ([]KYCAttempt, error) {
	return []KYCAttempt{}, nil
}

func (r *fakeOrgRepository) GetNotificationPrefs(_ context.Context, orgID string) (NotificationPrefs, error) {
	return NotificationPrefs{OrgID: orgID, PaymentUpdated: true, PaymentRefunded: true, PaymentExpired: true, WithdrawalUpdates: true, KYCDecisions: true}, nil
}

func (r *fakeOrgRepository) UpsertNotificationPrefs(_ context.Context, prefs NotificationPrefs) (NotificationPrefs, error) {
	return prefs, nil
}

func (r *fakeOrgRepository) PlatformStats(_ context.Context) (PlatformStats, error) {
	return PlatformStats{OrgsByKYC: map[string]int64{}, WithdrawalsByStatus: map[string]int64{}}, nil
}

func (r *fakeOrgRepository) UpdateOrgLogo(_ context.Context, orgID, logoURL string) (Organization, error) {
	return Organization{ID: orgID, LogoURL: logoURL}, nil
}

func (r *fakeOrgRepository) SuspendOrg(_ context.Context, orgID, reason string) (Organization, error) {
	return Organization{ID: orgID}, nil
}

func (r *fakeOrgRepository) UnsuspendOrg(_ context.Context, orgID string) (Organization, error) {
	return Organization{ID: orgID}, nil
}

func (r *fakeOrgRepository) OrgSuspensionByApp(_ context.Context, _ string) (bool, string, error) {
	return false, "", nil
}

func (r *fakeOrgRepository) WriteAudit(_ context.Context, _, _, _, _, _, _ string, _, _ map[string]any) error {
	return nil
}

func (r *fakeOrgRepository) ListAudit(_ context.Context, _, _ string, _, _ int) ([]AuditEntry, error) {
	return []AuditEntry{}, nil
}

func (r *fakeOrgRepository) DeleteOrganization(_ context.Context, _ string) error { return nil }

func (r *fakeOrgRepository) CountApps(_ context.Context, _ string) (int64, error) { return r.apps, nil }

func (r *fakeOrgRepository) GetAppOrgID(_ context.Context, _ string) (string, error) {
	return "org_test", nil
}

func (r *fakeOrgRepository) GetMember(_ context.Context, orgID, userID string) (OrgMember, error) {
	if r.memberErr != nil {
		return OrgMember{}, r.memberErr
	}
	member, ok := r.members[orgKey(orgID, userID)]
	if !ok {
		return OrgMember{}, ErrNotOrgMember
	}
	return member, nil
}

func (r *fakeOrgRepository) ListMembers(_ context.Context, _ string) ([]OrgMember, error) {
	return nil, nil
}

func (r *fakeOrgRepository) InviteMember(_ context.Context, orgID, userID string, role Role, invitedBy string) (OrgMember, error) {
	if _, ok := r.members[orgKey(orgID, userID)]; ok {
		return OrgMember{}, ErrAlreadyMember
	}
	member := OrgMember{OrgID: orgID, UserID: userID, Role: role, InvitedBy: invitedBy, Status: MemberStatusInvited}
	r.members[orgKey(orgID, userID)] = member
	r.invited = append(r.invited, member)
	return member, nil
}

func (r *fakeOrgRepository) AcceptInvite(_ context.Context, orgID, userID string) (OrgMember, error) {
	member, ok := r.members[orgKey(orgID, userID)]
	if !ok || member.Status != MemberStatusInvited {
		return OrgMember{}, ErrInviteNotFound
	}
	member.Status = MemberStatusActive
	r.members[orgKey(orgID, userID)] = member
	return member, nil
}

func (r *fakeOrgRepository) AddActiveMember(_ context.Context, orgID, userID string, role Role, addedBy string) (OrgMember, error) {
	if _, ok := r.members[orgKey(orgID, userID)]; ok {
		return OrgMember{}, ErrAlreadyMember
	}
	member := OrgMember{OrgID: orgID, UserID: userID, Role: role, InvitedBy: addedBy, Status: MemberStatusActive}
	r.members[orgKey(orgID, userID)] = member
	return member, nil
}

func (r *fakeOrgRepository) ListAppMembers(_ context.Context, _ string) ([]OrgMember, error) {
	return nil, nil
}

func (r *fakeOrgRepository) UpdateMemberRole(_ context.Context, orgID, userID string, role Role) (OrgMember, error) {
	member, ok := r.members[orgKey(orgID, userID)]
	if !ok {
		return OrgMember{}, ErrNotOrgMember
	}
	member.Role = role
	r.members[orgKey(orgID, userID)] = member
	r.updatedRoles = append(r.updatedRoles, [3]string{orgID, userID, string(role)})
	return member, nil
}

func (r *fakeOrgRepository) RemoveMember(_ context.Context, orgID, userID string) error {
	if _, ok := r.members[orgKey(orgID, userID)]; !ok {
		return ErrNotOrgMember
	}
	delete(r.members, orgKey(orgID, userID))
	r.removed = append(r.removed, [2]string{orgID, userID})
	return nil
}

func (r *fakeOrgRepository) CountOwners(_ context.Context, _ string) (int64, error) {
	return r.owners, nil
}

func (r *fakeOrgRepository) CountActiveMembers(_ context.Context, _ string) (int64, error) {
	return 1, nil
}

func (r *fakeOrgRepository) CountActiveOrgsForUser(_ context.Context, userID string) (int64, error) {
	var count int64
	for _, m := range r.members {
		if m.UserID == userID && m.Status == MemberStatusActive {
			count++
		}
	}
	return count, nil
}

func (r *fakeOrgRepository) FindUserIDByEmail(_ context.Context, email string) (string, error) {
	if r.userErr != nil {
		return "", r.userErr
	}
	if id, ok := r.userIDs[email]; ok {
		return id, nil
	}
	return "", ErrUserNotFound
}

func (r *fakeOrgRepository) SubmitKYC(_ context.Context, orgID, businessName, tin, docURL string) (KYCSubmission, error) {
	return KYCSubmission{OrgID: orgID, BusinessName: businessName, TIN: tin, IDDocumentURL: docURL}, nil
}
func (r *fakeOrgRepository) GetKYCSubmission(_ context.Context, orgID string) (KYCSubmission, bool, error) {
	if r.kycFound {
		return KYCSubmission{OrgID: orgID, BusinessName: "Acme", TIN: "123456789"}, true, nil
	}
	return KYCSubmission{}, false, nil
}

func (r *fakeOrgRepository) ReviewKYC(_ context.Context, orgID, status, reviewedBy, reason string) (Organization, error) {
	if r.reviewErr != nil {
		return Organization{}, r.reviewErr
	}
	r.reviewedStatus = status
	r.reviewedBy = reviewedBy
	r.reviewReason = reason
	return Organization{ID: orgID, Name: "Test Org", KYCStatus: status}, nil
}

func (r *fakeOrgRepository) ListKYCQueue(_ context.Context, status string) ([]KYCQueueItem, error) {
	if r.queueErr != nil {
		return nil, r.queueErr
	}
	if r.queue != nil {
		return r.queue, nil
	}
	return []KYCQueueItem{}, nil
}

func (r *fakeOrgRepository) UpdateOrgLiveLimits(_ context.Context, orgID, maxTxn, dailyCap string) (Organization, error) {
	r.limitsMaxTxn = maxTxn
	r.limitsDailyCap = dailyCap
	return Organization{ID: orgID, Name: "Test Org", LiveMaxTxnAmount: maxTxn, LiveDailyVolumeCap: dailyCap}, nil
}

func ownerRepo() *fakeOrgRepository {
	repo := newFakeOrgRepository()
	repo.members[orgKey("org_test", "owner-1")] = OrgMember{OrgID: "org_test", UserID: "owner-1", Role: RoleOwner, Status: MemberStatusActive}
	repo.owners = 1
	return repo
}

func TestCheckAppPermissionBlocksOutsideRole(t *testing.T) {
	repo := newFakeOrgRepository()
	repo.members[orgKey("org_test", "u-viewer")] = OrgMember{OrgID: "org_test", UserID: "u-viewer", Role: RoleViewer, Status: MemberStatusActive}
	service := NewService(repo, nil)

	if _, err := service.CheckAppPermission(context.Background(), "u-viewer", "app_test", PermWithdraw); err == nil {
		t.Fatal("expected viewer blocked from withdrawals")
	}
	if _, err := service.CheckAppPermission(context.Background(), "u-viewer", "app_test", PermRead); err != nil {
		t.Fatalf("expected viewer allowed reads, got %v", err)
	}
	if _, err := service.CheckAppPermission(context.Background(), "stranger", "app_test", PermRead); err == nil {
		t.Fatal("expected non-member blocked")
	}
}

func TestInviteRequiresOwnerAndExistingUser(t *testing.T) {
	repo := ownerRepo()
	repo.userIDs["new@example.com"] = "user-new"
	service := NewService(repo, nil)
	ctx := context.Background()

	if _, err := service.InviteMember(ctx, "owner-1", "org_test", "new@example.com", RoleFinance); err != nil {
		t.Fatalf("expected owner invite success, got %v", err)
	}

	repo.members[orgKey("org_test", "u-dev")] = OrgMember{OrgID: "org_test", UserID: "u-dev", Role: RoleDeveloper, Status: MemberStatusActive}
	if _, err := service.InviteMember(ctx, "u-dev", "org_test", "new@example.com", RoleViewer); err == nil {
		t.Fatal("expected developer blocked from inviting")
	}

	if _, err := service.InviteMember(ctx, "owner-1", "org_test", "ghost@example.com", RoleViewer); err == nil {
		t.Fatal("expected unknown email rejected")
	}
}

func TestChangeRoleProtectsLastOwner(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	if _, err := service.ChangeMemberRole(ctx, "owner-1", "org_test", "owner-1", RoleViewer); err == nil {
		t.Fatal("expected last-owner demotion blocked")
	}
	if len(repo.updatedRoles) != 0 {
		t.Fatal("expected no role write on blocked demotion")
	}

	repo.members[orgKey("org_test", "owner-2")] = OrgMember{OrgID: "org_test", UserID: "owner-2", Role: RoleOwner, Status: MemberStatusActive}
	repo.owners = 2
	if _, err := service.ChangeMemberRole(ctx, "owner-1", "org_test", "owner-2", RoleFinance); err != nil {
		t.Fatalf("expected demotion with backup owner to succeed, got %v", err)
	}
}

func TestRemoveMemberAndLeaveRules(t *testing.T) {
	repo := ownerRepo()
	service := NewService(repo, nil)
	ctx := context.Background()

	if err := service.RemoveMember(ctx, "owner-1", "org_test", "owner-1"); err == nil {
		t.Fatal("expected self-removal via remove blocked")
	}
	if err := service.LeaveOrganization(ctx, "owner-1", "org_test"); err == nil {
		t.Fatal("expected last-owner leave blocked")
	}

	repo.members[orgKey("org_test", "u-dev")] = OrgMember{OrgID: "org_test", UserID: "u-dev", Role: RoleDeveloper, Status: MemberStatusActive}
	if err := service.RemoveMember(ctx, "owner-1", "org_test", "u-dev"); err != nil {
		t.Fatalf("expected owner removal of developer, got %v", err)
	}
	if err := service.LeaveOrganization(ctx, "u-dev", "org_test"); err == nil {
		t.Fatal("expected leave of removed member blocked")
	}
}

func TestAcceptInvite(t *testing.T) {
	repo := ownerRepo()
	repo.members[orgKey("org_test", "user-new")] = OrgMember{OrgID: "org_test", UserID: "user-new", Role: RoleViewer, Status: MemberStatusInvited}
	service := NewService(repo, nil)

	member, err := service.AcceptInvite(context.Background(), "user-new", "org_test")
	if err != nil {
		t.Fatalf("expected accept success, got %v", err)
	}
	if member.Status != MemberStatusActive {
		t.Fatalf("expected active membership, got %q", member.Status)
	}
	if _, err := service.AcceptInvite(context.Background(), "stranger", "org_test"); err == nil {
		t.Fatal("expected accept without invite blocked")
	}
}

func TestDeleteOrganizationRefusesNonEmpty(t *testing.T) {
	repo := ownerRepo()
	repo.apps = 2
	service := NewService(repo, nil)

	if err := service.DeleteOrganization(context.Background(), "owner-1", "org_test"); err == nil {
		t.Fatal("expected non-empty org deletion blocked")
	}

	repo.apps = 0
	if err := service.DeleteOrganization(context.Background(), "owner-1", "org_test"); err != nil {
		t.Fatalf("expected empty org deletion, got %v", err)
	}
}

func TestSingleOrgPerAccount(t *testing.T) {
	repo := ownerRepo() // owner-1 already active in org_test
	repo.userIDs["teammate@example.com"] = "user-team"
	service := NewService(repo, nil)
	ctx := context.Background()

	if _, _, err := service.CreateOrganization(ctx, "owner-1", "Second", ""); err == nil {
		t.Fatal("expected second org creation blocked")
	}
	if _, _, err := service.CreateOrganization(ctx, "fresh-user", "First", ""); err != nil {
		t.Fatalf("expected first org creation allowed, got %v", err)
	}
	if _, err := service.InviteMember(ctx, "owner-1", "org_test", "teammate@example.com", RoleViewer); err != nil {
		t.Fatalf("expected invite of org-less user allowed, got %v", err)
	}
	repo.members[orgKey("org_other", "user-team")] = OrgMember{OrgID: "org_other", UserID: "user-team", Role: RoleViewer, Status: MemberStatusActive}
	if _, err := service.AcceptInvite(ctx, "user-team", "org_test"); err == nil {
		t.Fatal("expected accept with existing active org blocked")
	}
}
