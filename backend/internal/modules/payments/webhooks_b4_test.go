package payments

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"lipago/internal/modules/payments/provider"
)

func TestEndpointSigningSecretVersions(t *testing.T) {
	svc, _ := newServiceWithFake(t, ServiceOptions{})

	v1a := svc.endpointSigningSecret("ep_test", 1)
	v1b := svc.endpointSigningSecret("ep_test", 1)
	if v1a == "" || v1a != v1b {
		t.Fatal("v1 derivation must be deterministic and non-empty")
	}
	// Version <= 0 normalizes to the legacy v1 derivation (fakes, and any
	// row predating the version column).
	if v0 := svc.endpointSigningSecret("ep_test", 0); v0 != v1a {
		t.Fatal("version 0 must equal the legacy v1 secret")
	}
	v2 := svc.endpointSigningSecret("ep_test", 2)
	v3 := svc.endpointSigningSecret("ep_test", 3)
	if v2 == v1a || v3 == v1a || v2 == v3 {
		t.Fatal("rotated versions must each yield a distinct secret")
	}
	if other := svc.endpointSigningSecret("ep_other", 2); other == v2 {
		t.Fatal("secrets must be scoped per endpoint")
	}
}

func TestRotateWebhookEndpointSecret(t *testing.T) {
	svc, repo := newServiceWithFake(t, ServiceOptions{})
	repo.endpoint = PaymentWebhookEndpoint{ID: "ep_test", AppID: "app_test", URL: "https://example.com/hook", Status: "active", SecretVersion: 1}

	before := svc.endpointSigningSecret("ep_test", 1)
	result, err := svc.RotateWebhookEndpointSecret(context.Background(), "app_test", "ep_test")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if result.Endpoint.SecretVersion != 2 {
		t.Fatalf("version = %d, want 2", result.Endpoint.SecretVersion)
	}
	if result.SigningSecret == "" || result.SigningSecret == before {
		t.Fatal("rotation must issue a fresh secret")
	}
	if result.SigningSecret != svc.endpointSigningSecret("ep_test", 2) {
		t.Fatal("returned secret must match the v2 derivation")
	}

	again, err := svc.RotateWebhookEndpointSecret(context.Background(), "app_test", "ep_test")
	if err != nil {
		t.Fatalf("second rotate: %v", err)
	}
	if again.Endpoint.SecretVersion != 3 || again.SigningSecret == result.SigningSecret {
		t.Fatalf("second rotation must advance again, got %+v", again.Endpoint)
	}
}

func TestRotateWebhookEndpointSecretErrors(t *testing.T) {
	svc, repo := newServiceWithFake(t, ServiceOptions{})

	repo.webhookEndpointErr = ErrPaymentWebhookEndpointNotFound
	if _, err := svc.RotateWebhookEndpointSecret(context.Background(), "app_test", "ghost"); !errors.Is(err, ErrPaymentWebhookEndpointNotFound) {
		t.Fatalf("expected not-found, got %v", err)
	}

	repo.webhookEndpointErr = nil
	repo.endpoint = PaymentWebhookEndpoint{ID: "ep_test", AppID: "app_test", Status: "active", SecretVersion: 1}
	repo.rotateSecretErr = ErrSecretVersionConflict
	if _, err := svc.RotateWebhookEndpointSecret(context.Background(), "app_test", "ep_test"); !errors.Is(err, ErrSecretVersionConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
}

func TestProcessDueDeliveriesSignsWithJobVersion(t *testing.T) {
	var receivedSignature string
	var receivedTimestamp string
	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSignature = r.Header.Get("X-LipaGO-Signature")
		receivedTimestamp = r.Header.Get("X-LipaGO-Timestamp")
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	repo := &fakePaymentRepository{
		deliveryJobs: []PaymentWebhookDeliveryJob{
			{
				ID:                    "del_v2",
				EventID:               "evt_v2",
				EndpointID:            "endpoint_v2",
				EndpointURL:           server.URL,
				EndpointSecretVersion: 2,
				AppID:                 "app_test",
				AttemptCount:          1,
				EventType:             "payment.updated",
				Provider:              "sonicpesa",
				ReceivedAt:            time.Now().UTC(),
				PaymentOrder: PaymentOrder{
					ID: "pay_v2", Provider: "sonicpesa", Status: provider.StatusPaid,
					Amount: "100.00", Currency: "TZS",
				},
			},
		},
	}
	opts := testServiceOptions()
	opts.HTTPClient = server.Client()
	service := NewService(repo, nil, testCipher, opts, nil)

	if _, err := service.ProcessDueDeliveries(context.Background(), 10); err != nil {
		t.Fatalf("process: %v", err)
	}
	want := signDeliveryPayload(service.endpointSigningSecret("endpoint_v2", 2), receivedTimestamp, receivedBody)
	if receivedSignature == "" || receivedSignature != want {
		t.Fatal("delivery must be signed with the job's secret version")
	}
	if stale := signDeliveryPayload(service.endpointSigningSecret("endpoint_v2", 1), receivedTimestamp, receivedBody); receivedSignature == stale {
		t.Fatal("delivery must not use the legacy v1 secret for a v2 job")
	}
}
