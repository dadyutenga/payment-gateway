package analytics

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// Service is a thin passthrough over the Repository plus CSV export
// builders. All endpoints are read-only.
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

func (s *Service) Overview(ctx context.Context, p Params) (Overview, error) {
	return s.repo.Overview(ctx, p)
}

func (s *Service) ProviderStats(ctx context.Context, p Params) ([]ProviderStat, error) {
	return s.repo.ProviderStats(ctx, p)
}

func (s *Service) TopMerchants(ctx context.Context, p Params, sortBy string) (MerchantList, error) {
	return s.repo.TopMerchants(ctx, p, sortBy)
}

func (s *Service) SignupFunnel(ctx context.Context, p Params) (Funnel, error) {
	return s.repo.SignupFunnel(ctx, p)
}

func (s *Service) DormantMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error) {
	return s.repo.DormantMerchants(ctx, p)
}

func (s *Service) ChurnRiskMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error) {
	return s.repo.ChurnRiskMerchants(ctx, p)
}

func (s *Service) FailureBreakdown(ctx context.Context, p Params) ([]FailureRow, error) {
	return s.repo.FailureBreakdown(ctx, p)
}

func (s *Service) WithdrawalStats(ctx context.Context, p Params) (WithdrawalStats, error) {
	return s.repo.WithdrawalStats(ctx, p)
}

func (s *Service) WebhookHealth(ctx context.Context, p Params) (WebhookHealth, error) {
	return s.repo.WebhookHealth(ctx, p)
}

func (s *Service) StuckOrders(ctx context.Context, p Params) ([]StuckOrder, error) {
	return s.repo.StuckOrders(ctx, p)
}

func (s *Service) Unreconciled(ctx context.Context, p Params, limit int) ([]UnreconciledItem, error) {
	return s.repo.Unreconciled(ctx, p, limit)
}

func (s *Service) NegativeBalances(ctx context.Context) ([]NegativeBalance, error) {
	return s.repo.NegativeBalances(ctx)
}

func (s *Service) LogExport(ctx context.Context, actorID, actorEmail, report, ip string) error {
	return s.repo.LogExport(ctx, actorID, actorEmail, report, ip)
}

// MerchantOverview reuses the platform Overview scoped to one org, plus
// average order value per currency.
func (s *Service) MerchantOverview(ctx context.Context, p Params, env string) (MerchantOverview, error) {
	overview, err := s.repo.Overview(ctx, p)
	if err != nil {
		return MerchantOverview{}, err
	}
	avg, err := s.repo.MerchantAvgOrderValue(ctx, p, env)
	if err != nil {
		return MerchantOverview{}, err
	}
	return MerchantOverview{Overview: overview, AvgOrderValue: avg}, nil
}

func (s *Service) MerchantChannels(ctx context.Context, p Params, env string) ([]ChannelRow, error) {
	return s.repo.MerchantChannels(ctx, p, env)
}

func (s *Service) MerchantPeakHours(ctx context.Context, p Params, env string) (PeakHours, error) {
	return s.repo.MerchantPeakHours(ctx, p, env)
}

func (s *Service) MerchantCustomers(ctx context.Context, p Params, env string, showDetail bool, mask func(string) string) (CustomerStats, error) {
	return s.repo.MerchantCustomers(ctx, p, env, showDetail, mask)
}

func (s *Service) MerchantApps(ctx context.Context, p Params, env string) ([]MerchantAppRow, error) {
	return s.repo.MerchantAppsTable(ctx, p, env)
}

func (s *Service) MerchantSettlements(ctx context.Context, p Params, env, currency string, entryLimit int) (Settlement, error) {
	return s.repo.MerchantSettlements(ctx, p, env, currency, entryLimit)
}

// ExportReports are the CSV-exportable list reports.
var ExportReports = []string{
	"top-merchants", "signups", "dormant", "churn-risk", "failures",
	"withdrawals", "webhook-offenders", "stuck-orders", "unreconciled",
	"negative-balances",
}

// ExportCSV builds a CSV for a list report. Money stays per-currency in
// dedicated columns (never summed across currencies).
func (s *Service) ExportCSV(ctx context.Context, report string, p Params) (filename string, data []byte, err error) {
	buf := &bytes.Buffer{}
	w := csv.NewWriter(buf)
	write := func(records [][]string) {
		for _, rec := range records {
			_ = w.Write(rec)
		}
	}
	switch report {
	case "top-merchants":
		list, err := s.repo.TopMerchants(ctx, p, "tx_count")
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"org_id", "org_name", "kyc_status", "tx_count", "success_rate", "refund_rate", "tpv_TZS", "revenue_TZS"}})
		for _, m := range list.Items {
			write([][]string{{
				m.OrgID, m.OrgName, m.KYCStatus, itoa(m.TxCount),
				rateStr(m.SuccessRate), rateStr(m.RefundRate),
				m.TPVByCurrency["TZS"], m.RevenueByCurrency["TZS"],
			}})
		}
	case "signups":
		funnel, err := s.repo.SignupFunnel(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"bucket", "signups", "emails_verified", "orgs_created", "kyc_submitted", "kyc_verified", "first_live_txn"}})
		for _, pt := range funnel.Series {
			write([][]string{{
				pt.Bucket, itoa(pt.Signups), itoa(pt.EmailsVerified), itoa(pt.OrgsCreated),
				itoa(pt.KYCSubmitted), itoa(pt.KYCVerified), itoa(pt.FirstLive),
			}})
		}
	case "dormant":
		items, err := s.repo.DormantMerchants(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"org_id", "org_name", "reason", "last_txn_at", "contact_email"}})
		for _, m := range items {
			write([][]string{{m.OrgID, m.OrgName, m.Reason, timeStr(m.LastTxnAt), m.ContactEmail}})
		}
	case "churn-risk":
		items, err := s.repo.ChurnRiskMerchants(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"org_id", "org_name", "reason", "drop_pct", "last_txn_at", "contact_email"}})
		for _, m := range items {
			write([][]string{{m.OrgID, m.OrgName, m.Reason, rateStr(m.DropPct), timeStr(m.LastTxnAt), m.ContactEmail}})
		}
	case "failures":
		items, err := s.repo.FailureBreakdown(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"code", "provider", "count", "affected_orgs", "sample_order_ids"}})
		for _, f := range items {
			write([][]string{{f.Code, f.Provider, itoa(f.Count), itoa(f.AffectedOrgs), strings.Join(f.SampleOrderIDs, ";")}})

		}
	case "withdrawals":
		stats, err := s.repo.WithdrawalStats(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"bucket", "count", "amounts_by_currency"}})
		for _, b := range stats.Aging {
			write([][]string{{b.Bucket, itoa(b.Count), flattenMoney(b.ByCurrency)}})
		}
	case "webhook-offenders":
		health, err := s.repo.WebhookHealth(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"endpoint_id", "app_id", "url", "failed", "last_error"}})
		for _, o := range health.Offenders {
			write([][]string{{o.EndpointID, o.AppID, o.URL, itoa(o.Failed), o.LastError}})
		}
	case "stuck-orders":
		items, err := s.repo.StuckOrders(ctx, p)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"order_id", "org_id", "org_name", "provider", "status", "amount", "currency", "age_minutes"}})
		for _, o := range items {
			write([][]string{{o.OrderID, o.OrgID, o.OrgName, o.Provider, o.Status, o.Amount, o.Currency, fmt.Sprintf("%.1f", o.AgeMinutes)}})
		}
	case "unreconciled":
		items, err := s.repo.Unreconciled(ctx, p, 200)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"kind", "order_id", "org_id", "amount", "currency", "age_hours"}})
		for _, u := range items {
			write([][]string{{u.Kind, u.OrderID, u.OrgID, u.Amount, u.Currency, fmt.Sprintf("%.1f", u.AgeHours)}})
		}
	case "negative-balances":
		items, err := s.repo.NegativeBalances(ctx)
		if err != nil {
			return "", nil, err
		}
		write([][]string{{"app_id", "app_name", "org_id", "org_name", "currency", "balance"}})
		for _, n := range items {
			write([][]string{{n.AppID, n.AppName, n.OrgID, n.OrgName, n.Currency, n.Balance}})
		}
	default:
		return "", nil, fmt.Errorf("unknown report %q (one of: %s)", report, strings.Join(ExportReports, ", "))
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return "", nil, fmt.Errorf("encode csv: %w", err)
	}
	return "lipago-" + report + ".csv", buf.Bytes(), nil
}

func itoa(n int64) string { return fmt.Sprintf("%d", n) }

func rateStr(f *float64) string {
	if f == nil {
		return ""
	}
	return fmt.Sprintf("%.2f", *f)
}

func timeStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func flattenMoney(m MoneyByCurrency) string {
	parts := make([]string, 0, len(m))
	for currency, amount := range m {
		parts = append(parts, amount+" "+currency)
	}
	sort.Strings(parts)
	return strings.Join(parts, "; ")
}
