package payments

import (
	"context"
	"strings"
	"testing"
)

func TestNormalizeAPIKeyEnvironment(t *testing.T) {
	for _, in := range []string{"live", "LIVE", " Live "} {
		if got := normalizeAPIKeyEnvironment(in); got != "live" {
			t.Errorf("%q → %q, want live", in, got)
		}
	}
	for _, in := range []string{"", "sandbox", "production", "rotating"} {
		if got := normalizeAPIKeyEnvironment(in); got != "sandbox" {
			t.Errorf("%q → %q, want sandbox", in, got)
		}
	}
}

func TestNormalizeAPIKeyLabel(t *testing.T) {
	label, errs := normalizeAPIKeyLabel("  production server  ")
	if errs.Any() {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if label != "production server" {
		t.Fatalf("label = %q, want trimmed", label)
	}
	long := strings.Repeat("x", 61)
	if _, errs := normalizeAPIKeyLabel(long); !errs.Any() {
		t.Fatal("61-char label should fail validation")
	} else if _, ok := errs["label"]; !ok {
		t.Fatalf("error should be on label field, got %v", errs)
	}
	if _, errs := normalizeAPIKeyLabel(""); errs.Any() {
		t.Fatalf("empty label must be allowed, got %v", errs)
	}
}

func TestCreateAppInitialKeyEnvironment(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		{"live", "live"},
		{"LIVE", "live"},
		{"", "sandbox"},
		{"sandbox", "sandbox"},
		{"weird", "sandbox"},
	} {
		svc, repo := newServiceWithFake(t, ServiceOptions{})
		_, _, err := svc.CreateApp(context.Background(), CreatePaymentAppInput{
			Name: "Shop", OrgID: "org_test", InitialKeyEnvironment: tc.in,
		})
		if err != nil {
			t.Fatalf("in=%q: %v", tc.in, err)
		}
		if repo.createdKeyEnv != tc.want {
			t.Errorf("in=%q: key env = %q, want %q", tc.in, repo.createdKeyEnv, tc.want)
		}
	}
}

func TestCreateAppAPIKeyLabel(t *testing.T) {
	svc, repo := newServiceWithFake(t, ServiceOptions{})
	result, err := svc.CreateAppAPIKey(context.Background(), "app_test", "sandbox", "staging")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if repo.createdKeyLabel != "staging" {
		t.Fatalf("label = %q, want staging", repo.createdKeyLabel)
	}
	if result.Key.Label != "" {
		// Fake ListAppAPIKeys synthesizes from created fields minus label;
		// the stored label is what matters.
		t.Fatalf("unexpected label on synthesized key: %q", result.Key.Label)
	}

	if _, err := svc.CreateAppAPIKey(context.Background(), "app_test", "live", strings.Repeat("y", 61)); err == nil {
		t.Fatal("over-long label should error")
	} else if !strings.Contains(err.Error(), "Label must be") {
		t.Fatalf("wrong error: %v", err)
	}
}

func TestUpdateAPIKeyLabel(t *testing.T) {
	svc, repo := newServiceWithFake(t, ServiceOptions{})
	if vErrs, err := svc.UpdateAPIKeyLabel(context.Background(), "app_test", "key_1", "prod"); err != nil || vErrs.Any() {
		t.Fatalf("update: vErrs=%v err=%v", vErrs, err)
	}
	if repo.updatedLabel != "prod" || repo.updatedLabelKeyID != "key_1" || repo.updatedLabelAppID != "app_test" {
		t.Fatalf("unexpected label write: %+v", repo)
	}

	repo.updateLabelErr = ErrPaymentAPIKeyNotFound
	if _, err := svc.UpdateAPIKeyLabel(context.Background(), "app_test", "ghost", "x"); err != ErrPaymentAPIKeyNotFound {
		t.Fatalf("expected not-found, got %v", err)
	}

	if vErrs, err := svc.UpdateAPIKeyLabel(context.Background(), "app_test", "key_1", strings.Repeat("z", 61)); err != nil || !vErrs.Any() {
		t.Fatalf("expected validation errors, got vErrs=%v err=%v", vErrs, err)
	}
}

func TestUpdatePaymentAppValidation(t *testing.T) {
	svc, _ := newServiceWithFake(t, ServiceOptions{})
	app, vErrs, err := svc.UpdatePaymentApp(context.Background(), "app_test", "New Name", "desc")
	if err != nil || vErrs.Any() {
		t.Fatalf("update: vErrs=%v err=%v", vErrs, err)
	}
	if app.Name != "New Name" {
		t.Fatalf("name = %q", app.Name)
	}

	if _, vErrs, err := svc.UpdatePaymentApp(context.Background(), "app_test", "  ", ""); err != nil || !vErrs.Any() {
		t.Fatalf("expected name-required errors, got vErrs=%v err=%v", vErrs, err)
	}
}
