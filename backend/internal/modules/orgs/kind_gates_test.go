package orgs

import (
	"context"
	"errors"
	"testing"
)

// Kind gates resolve account_kind from the database on every call and
// reject the wrong track with 403-mapped errors — never silently.
func TestRequireAccountKindGates(t *testing.T) {
	ctx := context.Background()

	creator := creatorOrgRepo()
	service := NewService(creator, nil)
	if _, err := service.RequireCreatorOrg(ctx, "owner-1", "org_test", PermRead); err != nil {
		t.Fatalf("creator org should pass creator gate, got %v", err)
	}
	if _, err := service.RequireMerchantOrg(ctx, "owner-1", "org_test", PermManageMembers); !errors.Is(err, ErrNotMerchantOrg) {
		t.Fatalf("creator org should fail merchant gate with ErrNotMerchantOrg, got %v", err)
	}

	merchant := ownerRepo()
	merchantSvc := NewService(merchant, nil)
	if _, err := merchantSvc.RequireMerchantOrg(ctx, "owner-1", "org_test", PermManageMembers); err != nil {
		t.Fatalf("merchant org should pass merchant gate, got %v", err)
	}
	if _, err := merchantSvc.RequireCreatorOrg(ctx, "owner-1", "org_test", PermRead); !errors.Is(err, ErrNotCreatorOrg) {
		t.Fatalf("merchant org should fail creator gate with ErrNotCreatorOrg, got %v", err)
	}
}

// Creator accounts are personal single-member workspaces: team management
// (invite / role change / removal) is merchant-only.
func TestTeamManagementRejectsCreatorOrg(t *testing.T) {
	ctx := context.Background()
	repo := creatorOrgRepo()
	repo.userIDs["new@example.com"] = "user-2"
	service := NewService(repo, nil)

	if _, err := service.InviteMember(ctx, "owner-1", "org_test", "new@example.com", RoleViewer); !errors.Is(err, ErrNotMerchantOrg) {
		t.Fatalf("creator invite should fail with ErrNotMerchantOrg, got %v", err)
	}
	if len(repo.invited) != 0 {
		t.Fatal("rejected invite must not write a membership row")
	}
	// Seed an active second member directly, then prove role/removal gates.
	repo.members[orgKey("org_test", "user-2")] = OrgMember{OrgID: "org_test", UserID: "user-2", Role: RoleViewer, Status: MemberStatusActive}
	if _, err := service.ChangeMemberRole(ctx, "owner-1", "org_test", "user-2", RoleFinance); !errors.Is(err, ErrNotMerchantOrg) {
		t.Fatalf("creator role change should fail with ErrNotMerchantOrg, got %v", err)
	}
	if len(repo.updatedRoles) != 0 {
		t.Fatal("rejected role change must not write")
	}
	if err := service.RemoveMember(ctx, "owner-1", "org_test", "user-2"); !errors.Is(err, ErrNotMerchantOrg) {
		t.Fatalf("creator removal should fail with ErrNotMerchantOrg, got %v", err)
	}
	if len(repo.removed) != 0 {
		t.Fatal("rejected removal must not write")
	}
}

// Merchant baseline: team management still works on the business track.
func TestTeamManagementAllowsMerchantOrg(t *testing.T) {
	ctx := context.Background()
	repo := ownerRepo()
	repo.userIDs["new@example.com"] = "user-2"
	service := NewService(repo, nil)

	if _, err := service.InviteMember(ctx, "owner-1", "org_test", "new@example.com", RoleViewer); err != nil {
		t.Fatalf("merchant invite should succeed, got %v", err)
	}
}

// App-scoped kind gates resolve the app's org kind from the database:
// merchant apps pass the merchant gate, creator apps pass the creator
// gate, and each rejects the other track with a 403-mapped error.
func TestRequireAppKindGates(t *testing.T) {
	ctx := context.Background()

	merchantSvc := NewService(ownerRepo(), nil)
	if _, err := merchantSvc.RequireMerchantApp(ctx, "owner-1", "app_test", PermRead); err != nil {
		t.Fatalf("merchant app should pass merchant gate, got %v", err)
	}
	if _, err := merchantSvc.RequireCreatorApp(ctx, "owner-1", "app_test", PermRead); !errors.Is(err, ErrNotCreatorOrg) {
		t.Fatalf("merchant app should fail creator gate with ErrNotCreatorOrg, got %v", err)
	}

	creatorSvc := NewService(creatorOrgRepo(), nil)
	if _, err := creatorSvc.RequireCreatorApp(ctx, "owner-1", "app_test", PermRead); err != nil {
		t.Fatalf("creator app should pass creator gate, got %v", err)
	}
	if _, err := creatorSvc.RequireMerchantApp(ctx, "owner-1", "app_test", PermRead); !errors.Is(err, ErrNotMerchantOrg) {
		t.Fatalf("creator app should fail merchant gate with ErrNotMerchantOrg, got %v", err)
	}

	// Unknown app surfaces as not-a-member (no kind leak).
	if _, err := NewService(newFakeOrgRepository(), nil).RequireMerchantApp(ctx, "ghost", "app_missing", PermRead); !errors.Is(err, ErrNotOrgMember) {
		t.Fatalf("unknown app should fail with ErrNotOrgMember, got %v", err)
	}
}
