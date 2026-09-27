package auth

import (
	"testing"
	"time"
)

func TestGenerateOTPCode(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := GenerateOTPCode()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if len(code) != 6 {
			t.Fatalf("code %q must be 6 digits", code)
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("code %q must be numeric", code)
			}
		}
	}
}

func TestValidTanzanianPhone(t *testing.T) {
	valid := []string{"+255712345678", "+255612345678", "+255781234567"}
	invalid := []string{
		"", "+254712345678", "+25571234567", "+2557123456789",
		"+255123456789", "0712345678", "+25571234567a", "255712345678",
	}
	for _, phone := range valid {
		if !ValidTanzanianPhone(phone) {
			t.Errorf("expected %q valid", phone)
		}
	}
	for _, phone := range invalid {
		if ValidTanzanianPhone(phone) {
			t.Errorf("expected %q invalid", phone)
		}
	}
}

func TestOTPConstants(t *testing.T) {
	if otpCodeLength != 6 {
		t.Fatalf("otpCodeLength = %d, want 6", otpCodeLength)
	}
	if otpTTL != 10*time.Minute {
		t.Fatalf("otpTTL = %v, want 10m", otpTTL)
	}
	if otpMaxPerHour != 5 || otpMaxAttempts != 5 {
		t.Fatalf("rate limits: perHour=%d attempts=%d, want 5/5", otpMaxPerHour, otpMaxAttempts)
	}
}
