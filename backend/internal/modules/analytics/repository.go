package analytics

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx"
)

// Repository is the analytics persistence boundary: read-only SQL over
// app.* tables. Live tables back every endpoint (rollups stay a cache
// until Block 6 proves the need); money totals always come from orders
// (gross) and the ledger (fees/refunds), per currency, live only.
type Repository interface {
	Overview(ctx context.Context, p Params) (Overview, error)
	ProviderStats(ctx context.Context, p Params) ([]ProviderStat, error)
	TopMerchants(ctx context.Context, p Params, sort string) (MerchantList, error)
	SignupFunnel(ctx context.Context, p Params) (Funnel, error)
	DormantMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error)
	ChurnRiskMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error)
	FailureBreakdown(ctx context.Context, p Params) ([]FailureRow, error)
	WithdrawalStats(ctx context.Context, p Params) (WithdrawalStats, error)
	WebhookHealth(ctx context.Context, p Params) (WebhookHealth, error)
	StuckOrders(ctx context.Context, p Params) ([]StuckOrder, error)
	Unreconciled(ctx context.Context, p Params, limit int) ([]UnreconciledItem, error)
	NegativeBalances(ctx context.Context) ([]NegativeBalance, error)
	LogExport(ctx context.Context, actorID, actorEmail, report, ip string) error
}

type PostgresRepository struct {
	db *pgx.ConnPool
}

func NewPostgresRepository(db *pgx.ConnPool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// condBuilder numbers $ placeholders as args accumulate.
type condBuilder struct {
	conds []string
	args  []any
}

func (b *condBuilder) ph(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", len(b.args))
}

func (b *condBuilder) where() string {
	if len(b.conds) == 0 {
		return "TRUE"
	}
	return strings.Join(b.conds, " AND ")
}

// orderRange filters an orders table aliased o on created_at.
func (b *condBuilder) orderRange(from, to time.Time) {
	b.conds = append(b.conds, "o.created_at >= "+b.ph(from), "o.created_at < "+b.ph(to))
}

// orderDims filters provider/currency and live-only by default.
func (b *condBuilder) orderDims(provider, currency string) {
	b.conds = append(b.conds, "o.environment = 'live'")
	if provider != "" {
		b.conds = append(b.conds, "o.provider = "+b.ph(provider))
	}
	if currency != "" {
		b.conds = append(b.conds, "o.currency = "+b.ph(currency))
	}
}

// orgScope joins apps (aliased a) and restricts to the given orgs. Callers
// must include "JOIN app.payment_apps a ON a.id = o.app_id" (or l.app_id
// for ledger queries via ledgerScope) when this adds conditions.
func (b *condBuilder) orgScope(orgIDs []string, appID, orgCol string) bool {
	needsJoin := len(orgIDs) > 0 || appID != ""
	if len(orgIDs) == 1 {
		b.conds = append(b.conds, orgCol+" = "+b.ph(orgIDs[0])+"::uuid")
	} else if len(orgIDs) > 1 {
		b.conds = append(b.conds, orgCol+" = ANY("+b.ph(orgIDs)+"::uuid[])")
	}
	return needsJoin
}

// bucketExpr buckets a timestamptz column into EAT text labels.
func bucketExpr(granularity, col string) string {
	tz := fmt.Sprintf("(%s AT TIME ZONE '%s')", col, Timezone)
	switch granularity {
	case GranularityHour:
		return fmt.Sprintf("to_char(%s, 'YYYY-MM-DD HH24:00')", tz)
	case GranularityWeek:
		return fmt.Sprintf("to_char(date_trunc('week', %s), 'YYYY-MM-DD')", tz)
	case GranularityMonth:
		return fmt.Sprintf("to_char(date_trunc('month', %s), 'YYYY-MM')", tz)
	default:
		return fmt.Sprintf("(%s)::date::text", tz)
	}
}

func floatPtr(f float64) *float64 { return &f }

func pctChange(curr, prev float64) *float64 {
	if prev == 0 {
		return nil
	}
	v := (curr - prev) / prev * 100
	return &v
}

// successRateSQL renders paid/(paid+failed+cancelled+reversed+expired).
const successRateSQL = `CASE WHEN (COUNT(*) FILTER (WHERE o.status = 'paid') + COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired'))) = 0 THEN NULL
	ELSE COUNT(*) FILTER (WHERE o.status = 'paid')::float / (COUNT(*) FILTER (WHERE o.status = 'paid') + COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired'))) END`

// Overview assembles KPIs, previous-period deltas, and the time series.
func (r *PostgresRepository) Overview(ctx context.Context, p Params) (Overview, error) {
	out := Overview{Timezone: Timezone, From: p.From, To: p.To}

	type kpis struct {
		tx                                                                 int64
		paid, failed, expired                                               int64
		success, abandon                                                    *float64
		medianTTP, p90TTP                                                   *float64
		active                                                              int64
		signups, orgs                                                       int64
		tpv, revenue                                                        map[string]string
	}
	query := func(from, to time.Time) (kpis, error) {
		var k kpis
		k.tpv, k.revenue = map[string]string{}, map[string]string{}
		b := &condBuilder{}
		b.orderRange(from, to)
		b.orderDims(p.Provider, "")
		joinApps := b.orgScope(p.OrgIDs, p.AppID, "a.org_id")
		if p.AppID != "" {
			b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
		}
		join := ""
		if joinApps {
			join = "JOIN app.payment_apps a ON a.id = o.app_id"
		}
		row := r.db.QueryRowEx(ctx, fmt.Sprintf(`
			SELECT COUNT(*),
			       COUNT(*) FILTER (WHERE o.status = 'paid'),
			       COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed')),
			       COUNT(*) FILTER (WHERE o.status = 'expired'),
			       %s,
			       COUNT(*) FILTER (WHERE o.status = 'expired')::float / NULLIF(COUNT(*), 0),
			       percentile_cont(0.5) WITHIN GROUP (ORDER BY fp.ttp_s),
			       percentile_cont(0.9) WITHIN GROUP (ORDER BY fp.ttp_s),
			       COUNT(DISTINCT CASE WHEN o.status = 'paid' THEN a2.org_id END)
			FROM app.payment_orders o
			%s
			LEFT JOIN (
				SELECT h.payment_order_id AS oid, EXTRACT(EPOCH FROM (MIN(h.created_at) - o2.created_at)) AS ttp_s
				FROM app.payment_status_history h
				JOIN app.payment_orders o2 ON o2.id = h.payment_order_id
				WHERE h.to_status = 'paid' AND o2.created_at >= $%d AND o2.created_at < $%d
				GROUP BY h.payment_order_id, o2.created_at
			) fp ON fp.oid = o.id
			LEFT JOIN app.payment_apps a2 ON a2.id = o.app_id
			WHERE %s`, successRateSQL, join, len(b.args)+1, len(b.args)+2, b.where()), nil, append(b.args, from, to)...)
		// NOTE: the fp subquery reuses the range bounds appended last.
		if err := row.Scan(&k.tx, &k.paid, &k.failed, &k.expired, &k.success, &k.abandon, &k.medianTTP, &k.p90TTP, &k.active); err != nil {
			return k, fmt.Errorf("overview kpis: %w", err)
		}
		// Per-currency money: gross from orders, net fees from the ledger.
		moneyArgs := append([]any{}, b.args...)
		money, err := r.queryMoneyByCurrency(ctx, fmt.Sprintf(`
			SELECT o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
			FROM app.payment_orders o %s WHERE %s GROUP BY o.currency`, join, b.where()), moneyArgs)
		if err != nil {
			return k, err
		}
		k.tpv = money
		rev, err := r.ledgerRevenueByCurrency(ctx, from, to, p)
		if err != nil {
			return k, err
		}
		k.revenue = rev
		if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.users WHERE created_at >= $1 AND created_at < $2`, nil, from, to).Scan(&k.signups); err != nil {
			return k, fmt.Errorf("overview signups: %w", err)
		}
		if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.organizations WHERE created_at >= $1 AND created_at < $2`, nil, from, to).Scan(&k.orgs); err != nil {
			return k, fmt.Errorf("overview orgs: %w", err)
		}
		return k, nil
	}

	curr, err := query(p.From, p.To)
	if err != nil {
		return out, err
	}
	prevFrom, prevTo := p.PreviousPeriod()
	prev, err := query(prevFrom, prevTo)
	if err != nil {
		return out, err
	}
	out.TxCount, out.SuccessRate, out.AbandonmentRate = curr.tx, curr.success, curr.abandon
	out.MedianTTPS, out.P90TTPS = curr.medianTTP, curr.p90TTP
	out.ActiveMerchants, out.NewSignups, out.NewOrgs = curr.active, curr.signups, curr.orgs
	out.TPV, out.Revenue = curr.tpv, curr.revenue
	sumMoney := func(m map[string]string) float64 {
		var total float64
		for _, v := range m {
			var f float64
			fmt.Sscanf(v, "%f", &f)
			total += f
		}
		return total
	}
	out.Delta = OverviewDelta{
		TPVpct:     pctChange(sumMoney(curr.tpv), sumMoney(prev.tpv)),
		RevenuePct: pctChange(sumMoney(curr.revenue), sumMoney(prev.revenue)),
		TxPct:      pctChange(float64(curr.tx), float64(prev.tx)),
	}

	// Time series (live orders, EAT buckets).
	sb := &condBuilder{}
	sb.orderRange(p.From, p.To)
	sb.orderDims(p.Provider, "")
	joinApps := sb.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		sb.conds = append(sb.conds, "o.app_id = "+sb.ph(p.AppID)+"::uuid")
	}
	join := ""
	if joinApps {
		join = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT %s AS bucket, COUNT(*),
		       %s,
		       MIN(o.created_at)
		FROM app.payment_orders o %s WHERE %s
		GROUP BY 1 ORDER BY MIN(o.created_at)`, bucketExpr(p.Granularity, "o.created_at"), successRateSQL, join, sb.where()), nil, sb.args...)
	if err != nil {
		return out, fmt.Errorf("overview series: %w", err)
	}
	defer rows.Close()
	out.Series = []OverviewPoint{}
	for rows.Next() {
		var pt OverviewPoint
		var minCreated time.Time
		if err := rows.Scan(&pt.Bucket, &pt.TxCount, &pt.SuccessRate, &minCreated); err != nil {
			return out, fmt.Errorf("scan overview series: %w", err)
		}
		pt.GrossByCurrency = MoneyByCurrency{}
		out.Series = append(out.Series, pt)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate overview series: %w", err)
	}
	// Gross per bucket per currency (second pass keeps the main query lean).
	gb := &condBuilder{}
	gb.orderRange(p.From, p.To)
	gb.orderDims(p.Provider, "")
	gJoinApps := gb.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		gb.conds = append(gb.conds, "o.app_id = "+gb.ph(p.AppID)+"::uuid")
	}
	gJoin := ""
	if gJoinApps {
		gJoin = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	grows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT %s AS bucket, o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2`, bucketExpr(p.Granularity, "o.created_at"), gJoin, gb.where()), nil, gb.args...)
	if err != nil {
		return out, fmt.Errorf("overview series gross: %w", err)
	}
	bucketGross := map[string]MoneyByCurrency{}
	for grows.Next() {
		var bucket, currency, gross string
		if err := grows.Scan(&bucket, &currency, &gross); err != nil {
			grows.Close()
			return out, fmt.Errorf("scan overview series gross: %w", err)
		}
		if bucketGross[bucket] == nil {
			bucketGross[bucket] = MoneyByCurrency{}
		}
		bucketGross[bucket][currency] = gross
	}
	grows.Close()
	if err := grows.Err(); err != nil {
		return out, fmt.Errorf("iterate overview series gross: %w", err)
	}
	for i := range out.Series {
		out.Series[i].GrossByCurrency = bucketGross[out.Series[i].Bucket]
		if out.Series[i].GrossByCurrency == nil {
			out.Series[i].GrossByCurrency = MoneyByCurrency{}
		}
	}
	return out, rows.Err()
}

// queryMoneyByCurrency runs a (currency, amount::text) GROUP BY query.
func (r *PostgresRepository) queryMoneyByCurrency(ctx context.Context, query string, args []any) (map[string]string, error) {
	out := map[string]string{}
	rows, err := r.db.QueryEx(ctx, query, nil, args...)
	if err != nil {
		return out, fmt.Errorf("money by currency: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var currency, amount string
		if err := rows.Scan(&currency, &amount); err != nil {
			return out, fmt.Errorf("scan money by currency: %w", err)
		}
		out[currency] = amount
	}
	return out, rows.Err()
}

// ledgerRevenueByCurrency nets platform fees minus fee refunds per
// currency (live orders only — fee rows always link an order).
func (r *PostgresRepository) ledgerRevenueByCurrency(ctx context.Context, from, to time.Time, p Params) (map[string]string, error) {
	b := &condBuilder{}
	b.conds = append(b.conds, "l.created_at >= "+b.ph(from), "l.created_at < "+b.ph(to))
	b.conds = append(b.conds, "o.environment = 'live'")
	if p.Provider != "" {
		b.conds = append(b.conds, "o.provider = "+b.ph(p.Provider))
	}
	join := "JOIN app.payment_orders o ON o.id = l.payment_order_id"
	if len(p.OrgIDs) > 0 || p.AppID != "" {
		join += " JOIN app.payment_apps a ON a.id = l.app_id"
		if len(p.OrgIDs) == 1 {
			b.conds = append(b.conds, "a.org_id = "+b.ph(p.OrgIDs[0])+"::uuid")
		} else if len(p.OrgIDs) > 1 {
			b.conds = append(b.conds, "a.org_id = ANY("+b.ph(p.OrgIDs)+"::uuid[])")
		}
		if p.AppID != "" {
			b.conds = append(b.conds, "l.app_id = "+b.ph(p.AppID)+"::uuid")
		}
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT l.currency,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)::text,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0)::text
		FROM app.payment_ledger_entries l %s WHERE %s GROUP BY l.currency`, join, b.where()), nil, b.args...)
	if err != nil {
		return map[string]string{}, fmt.Errorf("ledger revenue: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var currency, fees, reversals string
		if err := rows.Scan(&currency, &fees, &reversals); err != nil {
			return out, fmt.Errorf("scan ledger revenue: %w", err)
		}
		out[currency] = subtractMoney(fees, reversals)
	}
	return out, rows.Err()
}

// subtractMoney computes a-b on numeric strings (ledger scale).
func subtractMoney(a, b string) string {
	var fa, fb float64
	fmt.Sscanf(a, "%f", &fa)
	fmt.Sscanf(b, "%f", &fb)
	return fmt.Sprintf("%.2f", fa-fb)
}

// ProviderStats aggregates per-provider volume, quality, latency, and
// channel mix over live orders in range.
func (r *PostgresRepository) ProviderStats(ctx context.Context, p Params) ([]ProviderStat, error) {
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.orderDims("", "")
	if p.Provider != "" {
		b.conds = append(b.conds, "o.provider = "+b.ph(p.Provider))
	}
	joinApps := b.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
	}
	join := ""
	if joinApps {
		join = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	where, args := b.where(), b.args
	// Base per-provider aggregates (volume per currency follows).
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider, COUNT(*),
		       %s,
		       percentile_cont(0.5) WITHIN GROUP (ORDER BY o.provider_latency_ms) FILTER (WHERE o.provider_latency_ms IS NOT NULL),
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY o.provider_latency_ms) FILTER (WHERE o.provider_latency_ms IS NOT NULL),
		       COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled') AND o.created_at >= NOW() - INTERVAL '1 hour'),
		       COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled') AND o.created_at >= NOW() - INTERVAL '24 hours'),
		       MAX(o.created_at) FILTER (WHERE o.status IN ('failed','cancelled'))
		FROM app.payment_orders o %s WHERE %s GROUP BY o.provider ORDER BY o.provider`, successRateSQL, join, where), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("provider stats: %w", err)
	}
	defer rows.Close()
	stats := []ProviderStat{}
	byProvider := map[string]*ProviderStat{}
	for rows.Next() {
		var s ProviderStat
		var lastFailure sql.NullTime
		if err := rows.Scan(&s.Provider, &s.TxCount, &s.SuccessRate, &s.MedianLatencyMs, &s.P95LatencyMs,
			&s.Failures1h, &s.Failures24h, &lastFailure); err != nil {
			return nil, fmt.Errorf("scan provider stats: %w", err)
		}
		if lastFailure.Valid {
			t := lastFailure.Time
			s.LastFailureAt = &t
		}
		s.VolumeByCurrency = MoneyByCurrency{}
		s.Failures = map[string]int64{}
		s.RevenueByCurrency = MoneyByCurrency{}
		s.ChannelSplit = map[string]int64{}
		stats = append(stats, s)
		byProvider[s.Provider] = &stats[len(stats)-1]
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider stats: %w", err)
	}
	// Per-provider per-currency paid volume.
	volRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider, o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2`, join, where), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("provider volume: %w", err)
	}
	for volRows.Next() {
		var provider, currency, gross string
		if err := volRows.Scan(&provider, &currency, &gross); err != nil {
			volRows.Close()
			return nil, fmt.Errorf("scan provider volume: %w", err)
		}
		if s, ok := byProvider[provider]; ok {
			s.VolumeByCurrency[currency] = gross
		}
	}
	volRows.Close()
	if err := volRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider volume: %w", err)
	}
	// Failure codes + channel split per provider.
	extraRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider, COALESCE(NULLIF(o.failure_code, ''), 'unspecified'), COALESCE(NULLIF(o.channel, ''), 'OTHER'), COUNT(*)
		FROM app.payment_orders o %s WHERE %s AND o.status IN ('failed','cancelled','expired') GROUP BY 1, 2, 3`, join, where), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("provider failures: %w", err)
	}
	for extraRows.Next() {
		var provider, code, channel string
		var n int64
		if err := extraRows.Scan(&provider, &code, &channel, &n); err != nil {
			extraRows.Close()
			return nil, fmt.Errorf("scan provider failures: %w", err)
		}
		if s, ok := byProvider[provider]; ok {
			s.Failures[code] += n
		}
	}
	extraRows.Close()
	if err := extraRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider failures: %w", err)
	}
	// Channel split over ALL orders per provider.
	chanRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider, COALESCE(NULLIF(o.channel, ''), 'OTHER'), COUNT(*)
		FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2`, join, where), nil, args...)
	if err != nil {
		return nil, fmt.Errorf("provider channels: %w", err)
	}
	for chanRows.Next() {
		var provider, channel string
		var n int64
		if err := chanRows.Scan(&provider, &channel, &n); err != nil {
			chanRows.Close()
			return nil, fmt.Errorf("scan provider channels: %w", err)
		}
		if s, ok := byProvider[provider]; ok {
			s.ChannelSplit[channel] += n
		}
	}
	chanRows.Close()
	if err := chanRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider channels: %w", err)
	}
	// Revenue per provider per currency (ledger fees net of fee reversals).
	lb := &condBuilder{}
	lb.conds = append(lb.conds, "l.created_at >= "+lb.ph(p.From), "l.created_at < "+lb.ph(p.To))
	lb.conds = append(lb.conds, "o.environment = 'live'")
	ljoin := "JOIN app.payment_orders o ON o.id = l.payment_order_id"
	if joinApps {
		ljoin += " JOIN app.payment_apps a ON a.id = l.app_id"
		if len(p.OrgIDs) == 1 {
			lb.conds = append(lb.conds, "a.org_id = "+lb.ph(p.OrgIDs[0])+"::uuid")
		} else if len(p.OrgIDs) > 1 {
			lb.conds = append(lb.conds, "a.org_id = ANY("+lb.ph(p.OrgIDs)+"::uuid[])")
		}
		if p.AppID != "" {
			lb.conds = append(lb.conds, "l.app_id = "+lb.ph(p.AppID)+"::uuid")
		}
	}
	if p.Provider != "" {
		lb.conds = append(lb.conds, "o.provider = "+lb.ph(p.Provider))
	}
	revRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider, l.currency,
		       (COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)
		        - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0))::text
		FROM app.payment_ledger_entries l %s WHERE %s GROUP BY 1, 2`, ljoin, lb.where()), nil, lb.args...)
	if err != nil {
		return nil, fmt.Errorf("provider revenue: %w", err)
	}
	for revRows.Next() {
		var provider, currency, net string
		if err := revRows.Scan(&provider, &currency, &net); err != nil {
			revRows.Close()
			return nil, fmt.Errorf("scan provider revenue: %w", err)
		}
		if s, ok := byProvider[provider]; ok {
			s.RevenueByCurrency[currency] = net
		}
	}
	revRows.Close()
	if err := revRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate provider revenue: %w", err)
	}
	return stats, nil
}

// TopMerchants ranks orgs with live orders in range. All orgs are fetched
// (tenant counts are small) and sorted/paginated in Go; TPV/revenue sorts
// use the requested currency (default TZS) to stay currency-honest.
func (r *PostgresRepository) TopMerchants(ctx context.Context, p Params, sortBy string) (MerchantList, error) {
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.orderDims(p.Provider, "")
	if len(p.OrgIDs) == 1 {
		b.conds = append(b.conds, "a.org_id = "+b.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		b.conds = append(b.conds, "a.org_id = ANY("+b.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
	}
	where, args := b.where(), b.args
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT a.org_id::text, org.name, org.kyc_status, COUNT(*), %s,
		       COUNT(*) FILTER (WHERE o.status = 'paid')
		FROM app.payment_orders o
		JOIN app.payment_apps a ON a.id = o.app_id
		JOIN app.organizations org ON org.id = a.org_id
		WHERE %s GROUP BY 1, 2, 3`, successRateSQL, where), nil, args...)
	if err != nil {
		return MerchantList{}, fmt.Errorf("top merchants: %w", err)
	}
	defer rows.Close()
	byOrg := map[string]*MerchantRow{}
	order := []string{}
	for rows.Next() {
		var row MerchantRow
		var paid int64
		if err := rows.Scan(&row.OrgID, &row.OrgName, &row.KYCStatus, &row.TxCount, &row.SuccessRate, &paid); err != nil {
			return MerchantList{}, fmt.Errorf("scan top merchants: %w", err)
		}
		row.TPVByCurrency = MoneyByCurrency{}
		row.RevenueByCurrency = MoneyByCurrency{}
		byOrg[row.OrgID] = &row
		order = append(order, row.OrgID)
	}
	if err := rows.Err(); err != nil {
		return MerchantList{}, fmt.Errorf("iterate top merchants: %w", err)
	}
	// Per-org per-currency paid gross.
	volRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT a.org_id::text, o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o
		JOIN app.payment_apps a ON a.id = o.app_id
		WHERE %s GROUP BY 1, 2`, where), nil, args...)
	if err != nil {
		return MerchantList{}, fmt.Errorf("top merchant volume: %w", err)
	}
	for volRows.Next() {
		var orgID, currency, gross string
		if err := volRows.Scan(&orgID, &currency, &gross); err != nil {
			volRows.Close()
			return MerchantList{}, fmt.Errorf("scan top merchant volume: %w", err)
		}
		if row, ok := byOrg[orgID]; ok {
			row.TPVByCurrency[currency] = gross
		}
	}
	volRows.Close()
	if err := volRows.Err(); err != nil {
		return MerchantList{}, fmt.Errorf("iterate top merchant volume: %w", err)
	}
	// Per-org revenue (ledger fees net of fee reversals) + refund rate.
	// Separate condition set: ledger timestamps with org scope via apps.
	lb := &condBuilder{}
	lb.conds = append(lb.conds, "l.created_at >= "+lb.ph(p.From), "l.created_at < "+lb.ph(p.To))
	lb.conds = append(lb.conds, "o.environment = 'live'")
	if p.Provider != "" {
		lb.conds = append(lb.conds, "o.provider = "+lb.ph(p.Provider))
	}
	if len(p.OrgIDs) == 1 {
		lb.conds = append(lb.conds, "a.org_id = "+lb.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		lb.conds = append(lb.conds, "a.org_id = ANY("+lb.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		lb.conds = append(lb.conds, "l.app_id = "+lb.ph(p.AppID)+"::uuid")
	}
	revRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT a.org_id::text, l.currency,
		       (COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)
		        - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0))::text,
		       COUNT(*) FILTER (WHERE l.entry_type = 'refund_debit'),
		       COUNT(*) FILTER (WHERE l.entry_type = 'payment_credit')
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s
		GROUP BY 1, 2`, lb.where()), nil, lb.args...)
	if err != nil {
		return MerchantList{}, fmt.Errorf("top merchant revenue: %w", err)
	}
	for revRows.Next() {
		var orgID, currency, net string
		var refunds, credits int64
		if err := revRows.Scan(&orgID, &currency, &net, &refunds, &credits); err != nil {
			revRows.Close()
			return MerchantList{}, fmt.Errorf("scan top merchant revenue: %w", err)
		}
		if row, ok := byOrg[orgID]; ok {
			row.RevenueByCurrency[currency] = net
			if credits > 0 {
				rate := float64(refunds) / float64(credits)
				if row.RefundRate == nil {
					row.RefundRate = &rate
				} else {
					combined := (*row.RefundRate + rate) / 2
					row.RefundRate = &combined
				}
			}
		}
	}
	revRows.Close()
	if err := revRows.Err(); err != nil {
		return MerchantList{}, fmt.Errorf("iterate top merchant revenue: %w", err)
	}
	// Count-based trend (currency-free): this window vs previous window.
	prevFrom, prevTo := p.PreviousPeriod()
	trendRows, err := r.db.QueryEx(ctx, `
		SELECT a.org_id::text,
		       COUNT(*) FILTER (WHERE o.created_at >= $1 AND o.created_at < $2),
		       COUNT(*) FILTER (WHERE o.created_at >= $3 AND o.created_at < $4)
		FROM app.payment_orders o
		JOIN app.payment_apps a ON a.id = o.app_id
		WHERE o.environment = 'live' AND o.created_at >= $3 AND o.created_at < $2
		GROUP BY 1`, nil, p.From, p.To, prevFrom, prevTo)
	if err != nil {
		return MerchantList{}, fmt.Errorf("top merchant trend: %w", err)
	}
	for trendRows.Next() {
		var orgID string
		var curr, prev int64
		if err := trendRows.Scan(&orgID, &curr, &prev); err != nil {
			trendRows.Close()
			return MerchantList{}, fmt.Errorf("scan top merchant trend: %w", err)
		}
		if row, ok := byOrg[orgID]; ok {
			row.TrendPct = pctChange(float64(curr), float64(prev))
		}
	}
	trendRows.Close()
	if err := trendRows.Err(); err != nil {
		return MerchantList{}, fmt.Errorf("iterate top merchant trend: %w", err)
	}
	out := MerchantList{Page: p.Page, PerPage: p.PerPage}
	items := []MerchantRow{}
	for _, id := range order {
		items = append(items, *byOrg[id])
	}
	rankCurrency := p.Currency
	if rankCurrency == "" {
		rankCurrency = "TZS"
	}
	moneyOf := func(m MoneyByCurrency) float64 {
		var f float64
		fmt.Sscanf(m[rankCurrency], "%f", &f)
		return f
	}
	switch sortBy {
	case "tpv":
		sort.Slice(items, func(i, j int) bool { return moneyOf(items[i].TPVByCurrency) > moneyOf(items[j].TPVByCurrency) })
	case "revenue":
		sort.Slice(items, func(i, j int) bool { return moneyOf(items[i].RevenueByCurrency) > moneyOf(items[j].RevenueByCurrency) })
	case "success_rate":
		sort.Slice(items, func(i, j int) bool {
			vi, vj := 0.0, 0.0
			if items[i].SuccessRate != nil {
				vi = *items[i].SuccessRate
			}
			if items[j].SuccessRate != nil {
				vj = *items[j].SuccessRate
			}
			return vi > vj
		})
	default:
		sort.Slice(items, func(i, j int) bool { return items[i].TxCount > items[j].TxCount })
	}
	out.Total = int64(len(items))
	start := (p.Page - 1) * p.PerPage
	if start < len(items) {
		end := start + p.PerPage
		if end > len(items) {
			end = len(items)
		}
		out.Items = items[start:end]
	}
	return out, nil
}

// SignupFunnel tracks the merchant journey per EAT day with totals and
// median hours between steps.
func (r *PostgresRepository) SignupFunnel(ctx context.Context, p Params) (Funnel, error) {
	out := Funnel{Timezone: Timezone, Series: []FunnelPoint{}}
	seriesQ := fmt.Sprintf(`
		WITH days AS (
			SELECT %s AS bucket, MIN(u.created_at) AS ord
			FROM app.users u WHERE u.created_at >= $1 AND u.created_at < $2 GROUP BY 1
		)
		SELECT d.bucket,
		       (SELECT COUNT(*) FROM app.users u WHERE %s = d.bucket AND u.created_at >= $1 AND u.created_at < $2),
		       (SELECT COUNT(*) FROM app.users u WHERE %s = d.bucket AND u.email_verified_at >= $1 AND u.email_verified_at < $2),
		       (SELECT COUNT(*) FROM app.organizations o WHERE %s = d.bucket AND o.created_at >= $1 AND o.created_at < $2),
		       (SELECT COUNT(*) FROM app.kyc_submissions s WHERE %s = d.bucket AND s.submitted_at >= $1 AND s.submitted_at < $2),
		       (SELECT COUNT(*) FROM app.kyc_submissions s JOIN app.organizations o ON o.id = s.org_id
		         WHERE %s = d.bucket AND s.reviewed_at >= $1 AND s.reviewed_at < $2 AND o.kyc_status = 'verified'),
		       (SELECT COUNT(DISTINCT a.org_id) FROM app.payment_orders o JOIN app.payment_apps a ON a.id = o.app_id
		         WHERE %s = d.bucket AND o.status = 'paid' AND o.environment = 'live'
		           AND o.created_at >= $1 AND o.created_at < $2
		           AND NOT EXISTS (SELECT 1 FROM app.payment_orders o2 JOIN app.payment_apps a2 ON a2.id = o2.app_id
		                           WHERE a2.org_id = a.org_id AND o2.status = 'paid' AND o2.environment = 'live' AND o2.created_at < $1))
		FROM days d GROUP BY d.bucket, d.ord ORDER BY d.ord`,
		bucketExpr(p.Granularity, "u.created_at"),
		bucketExpr(p.Granularity, "u.created_at"),
		bucketExpr(p.Granularity, "u.email_verified_at"),
		bucketExpr(p.Granularity, "o.created_at"),
		bucketExpr(p.Granularity, "s.submitted_at"),
		bucketExpr(p.Granularity, "s.reviewed_at"),
		bucketExpr(p.Granularity, "o.created_at"))
	rows, err := r.db.QueryEx(ctx, seriesQ, nil, p.From, p.To)
	if err != nil {
		return out, fmt.Errorf("signup funnel series: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var pt FunnelPoint
		if err := rows.Scan(&pt.Bucket, &pt.Signups, &pt.EmailsVerified, &pt.OrgsCreated, &pt.KYCSubmitted, &pt.KYCVerified, &pt.FirstLive); err != nil {
			return out, fmt.Errorf("scan signup funnel: %w", err)
		}
		out.Series = append(out.Series, pt)
		out.Totals.Signups += pt.Signups
		out.Totals.EmailsVerified += pt.EmailsVerified
		out.Totals.OrgsCreated += pt.OrgsCreated
		out.Totals.KYCSubmitted += pt.KYCSubmitted
		out.Totals.KYCVerified += pt.KYCVerified
		out.Totals.FirstLive += pt.FirstLive
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate signup funnel: %w", err)
	}
	if out.Totals.Signups > 0 {
		pct := float64(out.Totals.FirstLive) / float64(out.Totals.Signups) * 100
		out.Totals.SignupToLivePct = &pct
	}
	medianHours := func(query string) *float64 {
		var v *float64
		if err := r.db.QueryRowEx(ctx, query, nil, p.From, p.To).Scan(&v); err != nil {
			return nil
		}
		return v
	}
	out.MedianHours.RegisteredToEmail = medianHours(`
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (u.email_verified_at - u.created_at))/3600)
		FROM app.users u WHERE u.email_verified_at >= $1 AND u.email_verified_at < $2`)
	out.MedianHours.OrgToSubmitted = medianHours(`
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (s.submitted_at - o.created_at))/3600)
		FROM app.kyc_submissions s JOIN app.organizations o ON o.id = s.org_id
		WHERE s.submitted_at >= $1 AND s.submitted_at < $2`)
	out.MedianHours.SubmittedToDecided = medianHours(`
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (s.reviewed_at - s.submitted_at))/3600)
		FROM app.kyc_submissions s WHERE s.reviewed_at >= $1 AND s.reviewed_at < $2`)
	out.MedianHours.OrgToFirstLive = medianHours(`
		SELECT percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (first_live.first_at - o.created_at))/3600)
		FROM app.organizations o JOIN (
			SELECT a.org_id AS oid, MIN(o.created_at) AS first_at
			FROM app.payment_orders o JOIN app.payment_apps a ON a.id = o.app_id
			WHERE o.status = 'paid' AND o.environment = 'live' GROUP BY a.org_id
		) first_live ON first_live.oid = o.id
		WHERE first_live.first_at >= $1 AND first_live.first_at < $2`)
	return out, rows.Err()
}

// ownerContact returns the first active owner's email for list displays.
const ownerContactSQL = `COALESCE((SELECT u.email FROM app.org_members m JOIN app.users u ON u.id = m.user_id
	WHERE m.org_id = o.id AND m.role = 'owner' AND m.status = 'active'
	ORDER BY m.created_at ASC LIMIT 1), '')`

// DormantMerchants lists verified orgs with no paid live order in the
// last N days (or never), with contact email.
func (r *PostgresRepository) DormantMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(p.DormantDays) * 24 * time.Hour)
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.name, MAX(po.created_at),
		       %s
		FROM app.organizations o
		LEFT JOIN app.payment_apps a ON a.org_id = o.id AND a.status != 'deleted'
		LEFT JOIN app.payment_orders po ON po.app_id = a.id AND po.status = 'paid' AND po.environment = 'live'
		WHERE o.kyc_status = 'verified'
		GROUP BY o.id, o.name
		HAVING MAX(po.created_at) IS NULL OR MAX(po.created_at) < $1
		ORDER BY MAX(po.created_at) NULLS FIRST`, ownerContactSQL), nil, cutoff)
	if err != nil {
		return nil, fmt.Errorf("dormant merchants: %w", err)
	}
	defer rows.Close()
	out := []FlaggedMerchant{}
	for rows.Next() {
		var m FlaggedMerchant
		var lastTxn sql.NullTime
		if err := rows.Scan(&m.OrgID, &m.OrgName, &lastTxn, &m.ContactEmail); err != nil {
			return nil, fmt.Errorf("scan dormant merchants: %w", err)
		}
		if lastTxn.Valid {
			t := lastTxn.Time
			m.LastTxnAt = &t
			m.Reason = fmt.Sprintf("No paid live transactions in the last %d days (last: %s).", p.DormantDays, t.Format("2006-01-02"))
		} else {
			m.Reason = "Verified but never transacted live."
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// ChurnRiskMerchants flags orgs whose trailing-window paid-tx count
// dropped more than dropPct versus the prior window (prior >= 5 to cut
// noise), or with zero recent txns after a previously active pattern.
func (r *PostgresRepository) ChurnRiskMerchants(ctx context.Context, p Params) ([]FlaggedMerchant, error) {
	now := time.Now().UTC()
	recentFrom := now.Add(-time.Duration(p.ChurnWindowDays) * 24 * time.Hour)
	priorFrom := now.Add(-2 * time.Duration(p.ChurnWindowDays) * 24 * time.Hour)
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.id::text, o.name,
		       COUNT(*) FILTER (WHERE po.created_at >= $1 AND po.status = 'paid' AND po.environment = 'live'),
		       COUNT(*) FILTER (WHERE po.created_at >= $2 AND po.created_at < $1 AND po.status = 'paid' AND po.environment = 'live'),
		       MAX(po.created_at) FILTER (WHERE po.status = 'paid' AND po.environment = 'live'),
		       %s
		FROM app.organizations o
		LEFT JOIN app.payment_apps a ON a.org_id = o.id AND a.status != 'deleted'
		LEFT JOIN app.payment_orders po ON po.app_id = a.id
		GROUP BY o.id, o.name`, ownerContactSQL), nil, recentFrom, priorFrom)
	if err != nil {
		return nil, fmt.Errorf("churn risk merchants: %w", err)
	}
	defer rows.Close()
	out := []FlaggedMerchant{}
	for rows.Next() {
		var m FlaggedMerchant
		var recent, prior int64
		var lastTxn sql.NullTime
		if err := rows.Scan(&m.OrgID, &m.OrgName, &recent, &prior, &lastTxn, &m.ContactEmail); err != nil {
			return nil, fmt.Errorf("scan churn risk merchants: %w", err)
		}
		if lastTxn.Valid {
			t := lastTxn.Time
			m.LastTxnAt = &t
		}
		if prior < 5 {
			continue
		}
		drop := (float64(prior) - float64(recent)) / float64(prior) * 100
		if recent == 0 {
			m.Reason = fmt.Sprintf("Gone quiet: %d paid transactions in the prior %d days, zero since.", prior, p.ChurnWindowDays)
			m.DropPct = floatPtr(100)
			out = append(out, m)
			continue
		}
		if drop >= p.ChurnDropPct {
			m.Reason = fmt.Sprintf("Volume down %.0f%%: %d paid transactions in the prior %d days vs %d recently.", drop, prior, p.ChurnWindowDays, recent)
			m.DropPct = &drop
			out = append(out, m)
		}
	}
	return out, rows.Err()
}
