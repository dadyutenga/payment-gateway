package analytics

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Timezone states the bucketing zone in every analytics response.
const Timezone = "Africa/Dar_es_Salaam"

// Granularities for time series bucketing (EAT).
const (
	GranularityHour  = "hour"
	GranularityDay   = "day"
	GranularityWeek  = "week"
	GranularityMonth = "month"
)

// Guardrails: max lookback and page sizes (Block 6: 400 + clear message
// when exceeded).
const (
	MaxRangeDays = 366
	DefaultRange = 30 * 24 * time.Hour
	DefaultLimit = 50
	MaxLimit     = 200
)

// Threshold defaults (overridable per request, echoed in responses).
const (
	DefaultDormantDays      = 30
	DefaultChurnDropPct     = 50.0
	DefaultChurnWindowDays  = 14
	DefaultStuckMinutes     = 30
	DefaultWebhookStuckMins = 30
)

// Params are the common analytics query filters. OrgIDs scopes a query to
// specific orgs (merchant space); empty means platform-wide (admin only).
type Params struct {
	From        time.Time
	To          time.Time
	Granularity string
	Provider    string
	Currency    string
	OrgID       string
	OrgIDs      []string
	// AppID optionally narrows to one app (must belong to the org —
	// enforced server-side by the caller).
	AppID       string
	Page        int
	PerPage     int

	DormantDays     int
	ChurnDropPct    float64
	ChurnWindowDays int
	StuckMinutes    int
}

// PreviousPeriod returns the immediately preceding window of equal length.
func (p Params) PreviousPeriod() (time.Time, time.Time) {
	length := p.To.Sub(p.From)
	return p.From.Add(-length), p.From
}

// ParseParams reads the shared query params (?from&to as YYYY-MM-DD,
// granularity, provider, currency, org_id, page, per_page). from/to
// default to the trailing 30 EAT days ending now.
func ParseParams(r *http.Request, allowOrgFilter bool) (Params, error) {
	q := r.URL.Query()
	now := time.Now().UTC()
	to := now
	from := now.Add(-DefaultRange)
	if raw := strings.TrimSpace(q.Get("to")); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return Params{}, errors.New("to must be YYYY-MM-DD")
		}
		// End of that EAT day.
		loc, _ := time.LoadLocation(Timezone)
		if loc == nil {
			loc = time.UTC
		}
		y, m, d := parsed.Date()
		to = time.Date(y, m, d, 0, 0, 0, 0, loc).Add(24 * time.Hour).UTC()
	}
	if raw := strings.TrimSpace(q.Get("from")); raw != "" {
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return Params{}, errors.New("from must be YYYY-MM-DD")
		}
		loc, _ := time.LoadLocation(Timezone)
		if loc == nil {
			loc = time.UTC
		}
		y, m, d := parsed.Date()
		from = time.Date(y, m, d, 0, 0, 0, 0, loc).UTC()
	}
	if !to.After(from) {
		return Params{}, errors.New("to must be after from")
	}
	if to.Sub(from).Hours()/24 > MaxRangeDays {
		return Params{}, fmt.Errorf("range exceeds %d days", MaxRangeDays)
	}
	granularity := strings.ToLower(strings.TrimSpace(q.Get("granularity")))
	if granularity == "" {
		granularity = GranularityDay
	}
	switch granularity {
	case GranularityHour, GranularityDay, GranularityWeek, GranularityMonth:
	default:
		return Params{}, errors.New("granularity must be hour, day, week, or month")
	}
	if granularity == GranularityHour && to.Sub(from) > 7*24*time.Hour {
		return Params{}, errors.New("hour granularity is capped to a 7-day range (use day for longer ranges)")
	}
	page := parsePositiveInt(q.Get("page"), 1)
	perPage := parsePositiveInt(q.Get("per_page"), DefaultLimit)
	if perPage > MaxLimit {
		perPage = MaxLimit
	}
	p := Params{
		From: from, To: to, Granularity: granularity,
		Provider: strings.TrimSpace(q.Get("provider")),
		Currency: strings.ToUpper(strings.TrimSpace(q.Get("currency"))),
		Page: page, PerPage: perPage,
		DormantDays:     parsePositiveInt(q.Get("dormant_days"), DefaultDormantDays),
		ChurnDropPct:    parsePositiveFloat(q.Get("churn_drop_pct"), DefaultChurnDropPct),
		ChurnWindowDays: parsePositiveInt(q.Get("churn_window_days"), DefaultChurnWindowDays),
		StuckMinutes:    parsePositiveInt(q.Get("stuck_minutes"), DefaultStuckMinutes),
	}
	if allowOrgFilter {
		if orgID := strings.TrimSpace(q.Get("org_id")); orgID != "" {
			p.OrgID = orgID
			p.OrgIDs = []string{orgID}
		}
		if appID := strings.TrimSpace(q.Get("app_id")); appID != "" {
			p.AppID = appID
		}
	}
	return p, nil
}

func parsePositiveInt(raw string, fallback int) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

func parsePositiveFloat(raw string, fallback float64) float64 {
	n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// Envelopes ----------------------------------------------------------------

type MoneyByCurrency map[string]string

type Overview struct {
	Timezone       string           `json:"timezone"`
	From           time.Time        `json:"from"`
	To             time.Time        `json:"to"`
	TPV            MoneyByCurrency  `json:"tpv"`
	Revenue        MoneyByCurrency  `json:"revenue"`
	TxCount        int64            `json:"tx_count"`
	SuccessRate    *float64         `json:"success_rate"`
	AbandonmentRate *float64        `json:"abandonment_rate"`
	MedianTTPS     *float64         `json:"median_ttp_s"`
	P90TTPS        *float64         `json:"p90_ttp_s"`
	ActiveMerchants int64           `json:"active_merchants"`
	NewSignups     int64            `json:"new_signups"`
	NewOrgs        int64            `json:"new_orgs"`
	Delta          OverviewDelta    `json:"previous_period"`
	Series         []OverviewPoint  `json:"series"`
}

type OverviewDelta struct {
	TPVpct     *float64 `json:"tpv_pct_change,omitempty"`
	RevenuePct *float64 `json:"revenue_pct_change,omitempty"`
	TxPct      *float64 `json:"tx_pct_change,omitempty"`
}

type OverviewPoint struct {
	Bucket         string           `json:"bucket"`
	TxCount        int64            `json:"tx_count"`
	SuccessRate    *float64         `json:"success_rate"`
	GrossByCurrency MoneyByCurrency `json:"gross_by_currency"`
}

type ProviderStat struct {
	Provider       string            `json:"provider"`
	VolumeByCurrency MoneyByCurrency `json:"volume_by_currency"`
	TxCount        int64             `json:"tx_count"`
	SuccessRate    *float64          `json:"success_rate"`
	Failures       map[string]int64  `json:"failures_by_code"`
	MedianLatencyMs *float64         `json:"median_latency_ms"`
	P95LatencyMs   *float64          `json:"p95_latency_ms"`
	Failures1h     int64             `json:"failures_1h"`
	Failures24h    int64             `json:"failures_24h"`
	LastFailureAt  *time.Time        `json:"last_failure_at,omitempty"`
	RevenueByCurrency MoneyByCurrency `json:"revenue_by_currency"`
	ChannelSplit   map[string]int64  `json:"channel_split"`
}

type MerchantRow struct {
	OrgID        string           `json:"org_id"`
	OrgName      string           `json:"org_name"`
	KYCStatus    string           `json:"kyc_status"`
	TPVByCurrency MoneyByCurrency `json:"tpv_by_currency"`
	RevenueByCurrency MoneyByCurrency `json:"revenue_by_currency"`
	TxCount      int64            `json:"tx_count"`
	SuccessRate  *float64         `json:"success_rate"`
	RefundRate    *float64         `json:"refund_rate"`
	TrendPct     *float64         `json:"trend_pct,omitempty"`
}

type MerchantList struct {
	Items []MerchantRow `json:"items"`
	Total int64         `json:"total"`
	Page  int           `json:"page"`
	PerPage int         `json:"per_page"`
}

type Funnel struct {
	Timezone string          `json:"timezone"`
	Series   []FunnelPoint   `json:"series"`
	Totals   FunnelTotals    `json:"totals"`
	MedianHours FunnelTiming `json:"median_hours_between_steps"`
}

type FunnelPoint struct {
	Bucket       string `json:"bucket"`
	Signups      int64  `json:"signups"`
	EmailsVerified int64 `json:"emails_verified"`
	OrgsCreated  int64  `json:"orgs_created"`
	KYCSubmitted int64  `json:"kyc_submitted"`
	KYCVerified  int64  `json:"kyc_verified"`
	FirstLive    int64  `json:"first_live_txn"`
}

type FunnelTotals struct {
	Signups      int64   `json:"signups"`
	EmailsVerified int64 `json:"emails_verified"`
	OrgsCreated  int64   `json:"orgs_created"`
	KYCSubmitted int64   `json:"kyc_submitted"`
	KYCVerified  int64   `json:"kyc_verified"`
	FirstLive    int64   `json:"first_live_txn"`
	SignupToLivePct *float64 `json:"signup_to_live_pct,omitempty"`
}

type FunnelTiming struct {
	RegisteredToEmail *float64 `json:"registered_to_email,omitempty"`
	RegisteredToOrg   *float64 `json:"registered_to_org,omitempty"`
	OrgToSubmitted    *float64 `json:"org_to_kyc_submitted,omitempty"`
	SubmittedToDecided *float64 `json:"submitted_to_decided,omitempty"`
	OrgToFirstLive    *float64 `json:"org_to_first_live,omitempty"`
}

type FlaggedMerchant struct {
	OrgID       string  `json:"org_id"`
	OrgName     string  `json:"org_name"`
	Reason      string  `json:"reason"`
	LastTxnAt   *time.Time `json:"last_txn_at,omitempty"`
	RecentGross string  `json:"recent_gross,omitempty"`
	PriorGross  string  `json:"prior_gross,omitempty"`
	DropPct     *float64 `json:"drop_pct,omitempty"`
	ContactEmail string `json:"contact_email,omitempty"`
}

type FailureRow struct {
	Code        string   `json:"code"`
	Provider    string   `json:"provider"`
	Count       int64    `json:"count"`
	AffectedOrgs int64   `json:"affected_orgs"`
	SampleOrderIDs []string `json:"sample_order_ids"`
}

type WithdrawalStats struct {
	Timezone       string              `json:"timezone"`
	PendingCount   int64               `json:"pending_count"`
	PendingByCurrency MoneyByCurrency  `json:"pending_by_currency"`
	ApprovalQueue  int64               `json:"approval_queue"`
	Aging          []AgingBucket       `json:"aging"`
	FailedPayouts  int64               `json:"failed_payouts"`
	AvgPayoutHours *float64            `json:"avg_payout_hours,omitempty"`
}

type AgingBucket struct {
	Bucket string           `json:"bucket"`
	Count  int64            `json:"count"`
	ByCurrency MoneyByCurrency `json:"by_currency"`
}

type WebhookHealth struct {
	Timezone        string  `json:"timezone"`
	SuccessRate     *float64 `json:"success_rate"`
	RetryRate       *float64 `json:"retry_rate"`
	P95LatencyS     *float64 `json:"p95_latency_s,omitempty"`
	StuckProcessing int64   `json:"stuck_processing"`
	Offenders       []WebhookOffender `json:"top_offenders"`
}

type WebhookOffender struct {
	EndpointID string `json:"endpoint_id"`
	URL        string `json:"url"`
	AppID      string `json:"app_id"`
	Failed     int64  `json:"failed"`
	LastError  string `json:"last_error,omitempty"`
}

type StuckOrder struct {
	OrderID   string  `json:"order_id"`
	AppID     string  `json:"app_id"`
	OrgID     string  `json:"org_id"`
	OrgName   string  `json:"org_name"`
	Provider  string  `json:"provider"`
	Status    string  `json:"status"`
	Amount    string  `json:"amount"`
	Currency  string  `json:"currency"`
	AgeMinutes float64 `json:"age_minutes"`
	UpdatedAt time.Time `json:"updated_at"`
}

type UnreconciledItem struct {
	Kind      string  `json:"kind"`
	OrderID   string  `json:"order_id"`
	AppID     string  `json:"app_id"`
	OrgID     string  `json:"org_id"`
	Amount    string  `json:"amount"`
	Currency  string  `json:"currency"`
	AgeHours  float64 `json:"age_hours"`
}

type NegativeBalance struct {
	AppID     string `json:"app_id"`
	AppName   string `json:"app_name"`
	OrgID     string `json:"org_id"`
	OrgName   string `json:"org_name"`
	Currency  string `json:"currency"`
	Balance   string `json:"balance"`
}
