package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"lipago/internal/modules/payments/provider"
)

// Analytics failure codes (normalized enum, stored on orders at final
// transitions — never free text in analytics).
const (
	FailureExpired          = "expired"
	FailureCancelled        = "cancelled"
	FailureProviderDeclined = "provider_declined"
	FailureTimeout          = "timeout"
	FailureSystemError      = "system_error"
)

// Analytics channels (normalized payer networks).
const (
	ChannelMPesa   = "M-PESA"
	ChannelAirtel  = "AIRTEL"
	ChannelTigo    = "TIGO"
	ChannelHalopesa = "HALOPESA"
	ChannelOther   = "OTHER"
)

// NormalizeChannel maps provider channel/network strings (e.g.
// "AIRTELMONEY", "M-Pesa", "tigo") to the analytics enum. Unknown or
// empty input maps to "" (caller leaves the column untouched) when
// nothing recognizable is present... except analytics prefers an explicit
// OTHER over NULL for grouping, so non-empty unrecognized input maps to
// OTHER while empty stays empty.
func NormalizeChannel(raw map[string]any) string {
	if len(raw) == 0 {
		return ""
	}
	for _, key := range []string{"channel", "network", "Channel", "Network", "payment_channel", "operator"} {
		value, _ := raw[key].(string)
		upper := strings.ToUpper(strings.TrimSpace(value))
		if upper == "" {
			continue
		}
		compact := strings.ReplaceAll(strings.ReplaceAll(upper, "-", ""), " ", "")
		switch {
		case strings.Contains(compact, "MPESA"):
			return ChannelMPesa
		case strings.Contains(compact, "AIRTEL"):
			return ChannelAirtel
		case strings.Contains(compact, "TIGO"):
			return ChannelTigo
		case strings.Contains(compact, "HALO"):
			return ChannelHalopesa
		default:
			return ChannelOther
		}
	}
	return ""
}

// FailureCodeFor maps a final order outcome to (code, message). message is
// already truncated by callers to 500 chars.
func FailureCodeFor(nextStatus, providerStatus string, callErr error) (string, string) {
	switch nextStatus {
	case "expired":
		return FailureExpired, "Order expired without payment."
	case "cancelled":
		return FailureCancelled, "Order cancelled."
	}
	if callErr != nil {
		if errors.Is(callErr, context.DeadlineExceeded) || strings.Contains(strings.ToLower(callErr.Error()), "deadline exceeded") {
			return FailureTimeout, "Provider request timed out."
		}
		return FailureSystemError, "Provider request failed."
	}
	if msg := strings.TrimSpace(providerStatus); msg != "" {
		return FailureProviderDeclined, msg
	}
	return FailureProviderDeclined, "Declined by provider."
}

// NormalizePayerPhone canonicalizes a payer phone for hashing: digits
// only, Tanzanian 0-prefix rewritten to 255 so 0712… and 255712… hash
// identically. Non-TZ numbers hash as bare digits.
func NormalizePayerPhone(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if len(d) == 10 && strings.HasPrefix(d, "0") {
		return "255" + d[1:]
	}
	return d
}

// HashPayerPhone returns the hex HMAC-SHA256 analytics identifier for a
// payer phone. The raw number never leaves the orders table.
func HashPayerPhone(phone, secret string) string {
	normalized := NormalizePayerPhone(phone)
	if normalized == "" || secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte("lipago-payer-v1:" + normalized))
	return hex.EncodeToString(mac.Sum(nil))
}

// MaskPhone renders a server-side masked identifier ("+255 7** *** 123")
// for top-payer tables. Short/invalid input masks fully.
func MaskPhone(phone string) string {
	digits := NormalizePayerPhone(phone)
	if len(digits) < 9 {
		return "***"
	}
	head := digits
	country := ""
	if len(digits) > 9 {
		country = "+" + digits[:len(digits)-9] + " "
		head = digits[len(digits)-9:]
	}
	return country + head[:1] + "** *** " + head[len(head)-3:]
}

// EatDay buckets a timestamp into an Africa/Dar_es_Salaam calendar day
// (YYYY-MM-DD). All analytics bucketing uses EAT and says so.
func EatDay(t time.Time) string {
	loc, err := time.LoadLocation("Africa/Dar_es_Salaam")
	if err != nil {
		return t.UTC().Format("2006-01-02")
	}
	return t.In(loc).Format("2006-01-02")
}

// TruncateMessage caps free text stored alongside analytics codes.
func TruncateMessage(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max]
}

// stampAnalytics best-effort writes analytics columns; failures only warn
// (analytics must never break the money path).
func (s *Service) stampAnalytics(ctx context.Context, orderID string, upd OrderAnalyticsUpdate) {
	if err := s.repo.UpdateOrderAnalytics(ctx, orderID, upd); err != nil {
		s.log.Warn("order analytics stamp failed", "payment_order_id", orderID, "error", err)
	}
}

// stampFinal stamps channel + latency always, plus a failure code/message
// when nextStatus is a final non-paid outcome (paid/reversed carry none).
func (s *Service) stampFinal(ctx context.Context, orderID string, nextStatus provider.Status, providerStatus, channel string, latencyMs *int64) {
	upd := OrderAnalyticsUpdate{LatencyMs: latencyMs}
	if strings.TrimSpace(channel) != "" {
		upd.Channel = &channel
	}
	if isFinalStatus(nextStatus) && nextStatus != provider.StatusPaid && nextStatus != provider.StatusReversed {
		code, msg := FailureCodeFor(string(nextStatus), providerStatus, nil)
		upd.FailureCode = &code
		upd.FailureMessage = &msg
	}
	s.stampAnalytics(ctx, orderID, upd)
}
