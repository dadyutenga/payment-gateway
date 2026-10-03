package notifications

import "testing"

func TestNotificationTemplatesCoverPersistedEventInventory(t *testing.T) {
	want := []string{
		"account.welcome", "payment.succeeded", "payment.failed", "payment.expired", "payment.refunded",
		"withdrawal.requested", "withdrawal.requires_approval", "withdrawal.approved", "withdrawal.rejected", "withdrawal.dispatched", "withdrawal.completed", "withdrawal.failed",
		"kyc.submitted", "kyc.verified", "kyc.rejected", "webhook.delivery_failed", "org.suspended", "org.unsuspended",
		"org.limits_changed", "org.fee_override_changed", "org.member_invited", "org.role_changed", "creator.contribution_received",
		"security.payout_destination_changed", "security.api_key_rotated", "security.api_key_grace_ending", "admin.kyc_queue", "admin.webhook_alert", "admin.reconciliation_alert", "admin.payment_failed", "admin.broadcast",
	}
	for _, eventType := range want {
		template, ok := templates[eventType]
		if !ok || template.Title == "" || template.Body == "" || !isSeverity(template.Severity) {
			t.Fatalf("event %q has no complete notification template: %+v", eventType, template)
		}
	}
}

func TestCriticalEventsAlwaysKeepInAppDelivery(t *testing.T) {
	for _, eventType := range []string{"payment.failed", "withdrawal.failed", "kyc.rejected", "org.suspended", "security.payout_destination_changed"} {
		if !isCritical(eventType, templates[eventType].Severity) {
			t.Errorf("%s should be critical", eventType)
		}
	}
	if isCritical("payment.succeeded", templates["payment.succeeded"].Severity) {
		t.Fatal("successful payment should not be treated as an alert-only event")
	}
}

func TestTemplateRenderingUsesEventData(t *testing.T) {
	got := render(templates["payment.succeeded"].Body, map[string]any{"amount": "12,000", "currency": "TZS"})
	if got != "Malipo ya 12,000 TZS yamepokelewa kwa mafanikio." {
		t.Fatalf("unexpected rendered notification: %q", got)
	}
}
