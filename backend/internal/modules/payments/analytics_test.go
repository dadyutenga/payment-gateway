package payments

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestNormalizeChannel(t *testing.T) {
	cases := []struct {
		raw  map[string]any
		want string
	}{
		{map[string]any{"channel": "AIRTELMONEY"}, ChannelAirtel},
		{map[string]any{"channel": "M-Pesa"}, ChannelMPesa},
		{map[string]any{"network": "tigo"}, ChannelTigo},
		{map[string]any{"Channel": "halotel"}, ChannelHalopesa},
		{map[string]any{"channel": "weirdnet"}, ChannelOther},
		{map[string]any{}, ""},
		{nil, ""},
		{map[string]any{"channel": 42}, ""},
	}
	for _, tc := range cases {
		if got := NormalizeChannel(tc.raw); got != tc.want {
			t.Fatalf("NormalizeChannel(%v) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

func TestFailureCodeFor(t *testing.T) {
	if code, _ := FailureCodeFor("expired", "", nil); code != FailureExpired {
		t.Fatalf("expired -> %q", code)
	}
	if code, _ := FailureCodeFor("cancelled", "", nil); code != FailureCancelled {
		t.Fatalf("cancelled -> %q", code)
	}
	if code, msg := FailureCodeFor("failed", "INSUFFICIENT_FUNDS", nil); code != FailureProviderDeclined || msg != "INSUFFICIENT_FUNDS" {
		t.Fatalf("failed -> %q %q", code, msg)
	}
	if code, _ := FailureCodeFor("failed", "", context.DeadlineExceeded); code != FailureTimeout {
		t.Fatalf("deadline -> %q", code)
	}
}

func TestHashPayerPhone(t *testing.T) {
	secret := "test-secret-at-least-32-chars!!!!!!"
	a := HashPayerPhone("0712345678", secret)
	b := HashPayerPhone("255712345678", secret)
	if a == "" || a != b {
		t.Fatalf("TZ normalization must hash identically, got %q vs %q", a, b)
	}
	if c := HashPayerPhone("0712345679", secret); c == a {
		t.Fatal("different phones must hash differently")
	}
	if h := HashPayerPhone("0712345678", "other-secret"); h == a {
		t.Fatal("different secrets must hash differently")
	}
	if HashPayerPhone("", secret) != "" || HashPayerPhone("0712345678", "") != "" {
		t.Fatal("empty phone/secret must yield empty hash")
	}
	if strings.Contains(a, "712345678") {
		t.Fatal("raw phone must never appear in the hash")
	}
}

func TestMaskPhone(t *testing.T) {
	if got := MaskPhone("+255712345123"); got != "+255 7** *** 123" {
		t.Fatalf("mask = %q", got)
	}
	if got := MaskPhone("abc"); got != "***" {
		t.Fatalf("short mask = %q", got)
	}
}

func TestEatDayBoundary(t *testing.T) {
	// EAT is UTC+3: 20:59Z is still the 28th in Dar, 21:00Z is the 29th.
	before := time.Date(2026, 9, 28, 20, 59, 0, 0, time.UTC)
	after := time.Date(2026, 9, 28, 21, 0, 0, 0, time.UTC)
	if EatDay(before) != "2026-09-28" {
		t.Fatalf("EatDay(20:59Z) = %q", EatDay(before))
	}
	if EatDay(after) != "2026-09-29" {
		t.Fatalf("EatDay(21:00Z) = %q", EatDay(after))
	}
}
