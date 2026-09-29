package analytics

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
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
	// Merchant (org-scoped) analytics. Callers pin OrgIDs to exactly the
	// caller's org after resolving membership server-side.
	MerchantAvgOrderValue(ctx context.Context, p Params, env string) (MoneyByCurrency, error)
	MerchantChannels(ctx context.Context, p Params, env string) ([]ChannelRow, error)
	MerchantPeakHours(ctx context.Context, p Params, env string) (PeakHours, error)
	MerchantCustomers(ctx context.Context, p Params, env string, showDetail bool, mask func(string) string) (CustomerStats, error)
	MerchantAppsTable(ctx context.Context, p Params, env string) ([]MerchantAppRow, error)
	MerchantSettlements(ctx context.Context, p Params, env, currency string, entryLimit int) (Settlement, error)
}

type PostgresRepository struct {
	db *pgx.ConnPool
}

func NewPostgresRepository(db *pgx.ConnPool) *PostgresRepository {
	return &PostgresRepository{db: db}
}

// condBuilder numbers $ placeholders as args accumulate. offset shifts
// numbering so two builders can share one query (second builder starts
// after the first builder's args).
type condBuilder struct {
	conds  []string
	args   []any
	offset int
}

func (b *condBuilder) ph(v any) string {
	b.args = append(b.args, v)
	return fmt.Sprintf("$%d", b.offset+len(b.args))
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

// splitRange divides [from, to) into whole EAT days (rollup-backed) and
// leftover live edges. Rollup rows cover whole days only — partial edges
// always read live tables so dashboards never over-count.
func splitRange(from, to time.Time) (wholeFrom, wholeTo time.Time, hasWhole bool, headFrom, headTo, tailFrom, tailTo time.Time, hasHead, hasTail bool) {
	loc, err := time.LoadLocation(Timezone)
	if err != nil {
		loc = time.UTC
	}
	todayStart := time.Now().In(loc)
	todayStart = time.Date(todayStart.Year(), todayStart.Month(), todayStart.Day(), 0, 0, 0, 0, loc)
	// Whole days fully inside [from, min(to, todayStart)).
	end := to
	if end.After(todayStart) {
		end = todayStart
	}
	fy, fm, fd := from.In(loc).Date()
	firstDay := time.Date(fy, fm, fd, 0, 0, 0, 0, loc)
	if firstDay.Before(from) {
		firstDay = firstDay.Add(24 * time.Hour)
	}
	ey, em, ed := end.In(loc).Date()
	lastDay := time.Date(ey, em, ed, 0, 0, 0, 0, loc)
	if !lastDay.After(firstDay) && !lastDay.Equal(firstDay) {
		lastDay = firstDay
	}
	// Whole days are [firstDay, lastDay) where lastDay is a day boundary <= end.
	// Recompute cleanly: iterate day starts d with d >= from-day-ceil and d+24h <= end.
	wholeFrom, wholeTo = firstDay, firstDay
	for d := firstDay; !d.After(end.Add(-time.Second)); d = d.Add(24 * time.Hour) {
		if (d.After(from) || d.Equal(from)) && !d.Add(24*time.Hour).After(end) {
			if !hasWhole {
				wholeFrom = d
				hasWhole = true
			}
			wholeTo = d.Add(24 * time.Hour)
		}
	}
	if from.Before(wholeFrom) && hasWhole {
		headFrom, headTo, hasHead = from, wholeFrom, true
	} else if !hasWhole && from.Before(end) {
		headFrom, headTo, hasHead = from, end, true
	}
	if hasWhole && wholeTo.Before(to) {
		tailFrom, tailTo, hasTail = wholeTo, to, true
	} else if !hasWhole && !hasHead && from.Before(to) {
		tailFrom, tailTo, hasTail = from, to, true
	}
	return wholeFrom, wholeTo, hasWhole, headFrom, headTo, tailFrom, tailTo, hasHead, hasTail
}

// successRateSQL renders paid/(paid+failed+cancelled+reversed+expired).
const successRateSQL = `CASE WHEN (COUNT(*) FILTER (WHERE o.status = 'paid') + COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired'))) = 0 THEN NULL
	ELSE COUNT(*) FILTER (WHERE o.status = 'paid')::float / (COUNT(*) FILTER (WHERE o.status = 'paid') + COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired'))) END`

// Overview assembles KPIs, previous-period deltas, and the time series.
// Whole EAT days read pre-aggregated rollups; partial edges and today
// read live tables (splitRange); TTP percentiles always read live
// first_paid_at values (exact). Independent pieces run concurrently.
func (r *PostgresRepository) Overview(ctx context.Context, p Params) (Overview, error) {
	out := Overview{Timezone: Timezone, From: p.From, To: p.To}
	prevFrom, prevTo := p.PreviousPeriod()
	// Independent pieces run concurrently (pool is goroutine-safe);
	// first error wins.
	var currAgg, prevAgg periodSums
	var currMed, currP90 *float64
	var currSignups, currOrgs int64
	var currSeries []OverviewPoint
	var pipelineErr error
	var mu sync.Mutex
	setErr := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if pipelineErr == nil {
			pipelineErr = err
		}
		mu.Unlock()
	}
	var wg sync.WaitGroup
	run := func(fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			fn()
		}()
	}
	run(func() { a, err := r.periodAggRange(ctx, p, p.From, p.To); mu.Lock(); currAgg = a; mu.Unlock(); setErr(err) })
	run(func() { a, err := r.periodAggRange(ctx, p, prevFrom, prevTo); mu.Lock(); prevAgg = a; mu.Unlock(); setErr(err) })
	run(func() { m, t, err := r.periodTTP(ctx, p, p.From, p.To); mu.Lock(); currMed, currP90 = m, t; mu.Unlock(); setErr(err) })
	run(func() {
		su, og, err := r.periodSignups(ctx, p.From, p.To)
		mu.Lock()
		currSignups, currOrgs = su, og
		mu.Unlock()
		setErr(err)
	})
	run(func() { s, err := r.periodSeries(ctx, p, p.From, p.To); mu.Lock(); currSeries = s; mu.Unlock(); setErr(err) })
	wg.Wait()
	if pipelineErr != nil {
		return out, pipelineErr
	}
	if currAgg.gross == nil {
		currAgg.gross = map[string]string{}
	}
	if currAgg.revenue == nil {
		currAgg.revenue = map[string]string{}
	}
	out.TxCount, out.SuccessRate, out.AbandonmentRate =
		currAgg.tx, successFromSums(currAgg.paid, currAgg.failed+currAgg.expired), abandonFromSums(currAgg.expired, currAgg.tx)
	out.MedianTTPS, out.P90TTPS = currMed, currP90
	out.ActiveMerchants, out.NewSignups, out.NewOrgs = int64(len(currAgg.orgs)), currSignups, currOrgs
	out.TPV, out.Revenue = currAgg.gross, currAgg.revenue
	prevAgg.gross = nonNilMap(prevAgg.gross)
	prevAgg.revenue = nonNilMap(prevAgg.revenue)
	out.Delta = OverviewDelta{
		TPVpct:     pctChange(sumMoney(currAgg.gross), sumMoney(prevAgg.gross)),
		RevenuePct: pctChange(sumMoney(currAgg.revenue), sumMoney(prevAgg.revenue)),
		TxPct:      pctChange(float64(currAgg.tx), float64(prevAgg.tx)),
	}
	out.Series = currSeries
	if out.Series == nil {
		out.Series = []OverviewPoint{}
	}
	return out, nil
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// periodSums are exact additive aggregates; rates derive from them so
// rollup and live chunks merge without rounding drift.
type periodSums struct {
	tx, paid, failed, expired int64
	gross                     map[string]string
	revenue                   map[string]string
	orgs                      map[string]bool
}

type periodAgg struct {
	sums              periodSums
	medianTTP, p90TTP *float64
	signups, orgs     int64
	series            []OverviewPoint
}

func successFromSums(paid, bad int64) *float64 {
	if paid+bad == 0 {
		return nil
	}
	return floatPtr(float64(paid) / float64(paid+bad))
}

func abandonFromSums(expired, tx int64) *float64 {
	if tx == 0 {
		return nil
	}
	return floatPtr(float64(expired) / float64(tx))
}

func addMoneyMap(dst, src map[string]string) {
	for c, v := range src {
		var a, b float64
		fmt.Sscanf(dst[c], "%f", &a)
		fmt.Sscanf(v, "%f", &b)
		dst[c] = fmt.Sprintf("%.2f", a+b)
	}
}

func sumMoney(m map[string]string) float64 {
	var total float64
	for _, v := range m {
		var f float64
		fmt.Sscanf(v, "%f", &f)
		total += f
	}
	return total
}

// liveSums runs the exact live-table aggregates for a (usually small)
// range: counts, per-currency gross, net revenue, active org set.
func (r *PostgresRepository) liveSums(ctx context.Context, p Params, from, to time.Time) (periodSums, error) {
	agg := periodSums{gross: map[string]string{}, revenue: map[string]string{}, orgs: map[string]bool{}}
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
		       COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired')),
		       COUNT(*) FILTER (WHERE o.status = 'expired')
		FROM app.payment_orders o %s WHERE %s`, join, b.where()), nil, b.args...)
	if err := row.Scan(&agg.tx, &agg.paid, &agg.failed, &agg.expired); err != nil {
		return agg, fmt.Errorf("live sums: %w", err)
	}
	gross, err := r.queryMoneyByCurrency(ctx, fmt.Sprintf(`
		SELECT o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o %s WHERE %s GROUP BY o.currency`, join, b.where()), append([]any{}, b.args...))
	if err != nil {
		return agg, err
	}
	agg.gross = gross
	agg.revenue, err = r.ledgerRevenueByCurrency(ctx, from, to, p)
	if err != nil {
		return agg, err
	}
	orgRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT DISTINCT a.org_id::text FROM app.payment_orders o
		JOIN app.payment_apps a ON a.id = o.app_id
		WHERE %s AND o.status = 'paid'`, b.where()), nil, b.args...)
	if err != nil {
		return agg, fmt.Errorf("live active orgs: %w", err)
	}
	defer orgRows.Close()
	for orgRows.Next() {
		var orgID string
		if err := orgRows.Scan(&orgID); err != nil {
			return agg, fmt.Errorf("scan live active orgs: %w", err)
		}
		agg.orgs[orgID] = true
	}
	if err := orgRows.Err(); err != nil {
		return agg, fmt.Errorf("iterate live active orgs: %w", err)
	}
	return agg, nil
}

// periodTTP runs the single exact TTP percentile query over a range
// (first_paid_at — no history join).
func (r *PostgresRepository) periodTTP(ctx context.Context, p Params, from, to time.Time) (*float64, *float64, error) {
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
	var median, p90 *float64
	err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT percentile_cont(0.5) WITHIN GROUP (
		         ORDER BY EXTRACT(EPOCH FROM (o.first_paid_at - o.created_at))
		       ) FILTER (WHERE o.status = 'paid' AND o.first_paid_at IS NOT NULL),
		       percentile_cont(0.9) WITHIN GROUP (
		         ORDER BY EXTRACT(EPOCH FROM (o.first_paid_at - o.created_at))
		       ) FILTER (WHERE o.status = 'paid' AND o.first_paid_at IS NOT NULL)
		FROM app.payment_orders o %s WHERE %s`, join, b.where()), nil, b.args...).Scan(&median, &p90)
	if err != nil {
		return nil, nil, fmt.Errorf("period ttp: %w", err)
	}
	return median, p90, nil
}

// periodSignups counts new users and orgs in a window.
func (r *PostgresRepository) periodSignups(ctx context.Context, from, to time.Time) (int64, int64, error) {
	var su, og int64
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.users WHERE created_at >= $1 AND created_at < $2`, nil, from, to).Scan(&su); err != nil {
		return 0, 0, fmt.Errorf("overview signups: %w", err)
	}
	if err := r.db.QueryRowEx(ctx, `SELECT COUNT(*) FROM app.organizations WHERE created_at >= $1 AND created_at < $2`, nil, from, to).Scan(&og); err != nil {
		return 0, 0, fmt.Errorf("overview orgs: %w", err)
	}
	return su, og, nil
}

// dateList restricts a DATE column to an explicit day list (pgx v3 cannot
// bind a text slice as a date array, so placeholders are inlined).
func dateList(b *condBuilder, col string, days []string) {
	phs := make([]string, 0, len(days))
	for _, d := range days {
		phs = append(phs, b.ph(d)+"::date")
	}
	b.conds = append(b.conds, col+" IN ("+strings.Join(phs, ",")+")")
}

// rollupCoverage returns which EAT days in [dayFrom, dayTo) have daily
// rows FOR THE CURRENT SCOPE (dims included). A day present only for
// other orgs still falls back to live reads — otherwise scoped queries
// would silently under-count on fresh/partial rollups.
func (r *PostgresRepository) rollupCoverage(ctx context.Context, p Params, dayFrom, dayTo time.Time) (map[string]bool, error) {
	present := map[string]bool{}
	b := &condBuilder{}
	b.conds = append(b.conds, "d.day >= "+b.ph(dayFrom.Format("2006-01-02"))+"::date")
	b.conds = append(b.conds, "d.day < "+b.ph(dayTo.Format("2006-01-02"))+"::date")
	rollupDimConds(b, p, "d")
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT DISTINCT d.day::text FROM app.analytics_daily_app d WHERE %s`, b.where()), nil, b.args...)
	if err != nil {
		return present, fmt.Errorf("rollup coverage: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var day string
		if err := rows.Scan(&day); err != nil {
			return present, fmt.Errorf("scan rollup coverage: %w", err)
		}
		present[day] = true
	}
	return present, rows.Err()
}

// rollupDimConds filters rollup rows (daily grain columns).
func rollupDimConds(b *condBuilder, p Params, alias string) {
	b.conds = append(b.conds, alias+".environment = 'live'")
	if p.Provider != "" {
		b.conds = append(b.conds, alias+".provider = "+b.ph(p.Provider))
	}
	if len(p.OrgIDs) == 1 {
		b.conds = append(b.conds, alias+".org_id = "+b.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		b.conds = append(b.conds, alias+".org_id = ANY("+b.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		b.conds = append(b.conds, alias+".app_id = "+b.ph(p.AppID)+"::uuid")
	}
}

// rollupSums aggregates whole EAT days (present ones only) from the daily
// grain: counts, per-currency gross, net revenue, active orgs.
func (r *PostgresRepository) rollupSums(ctx context.Context, p Params, dayFrom, dayTo time.Time, present map[string]bool) (periodSums, error) {
	agg := periodSums{gross: map[string]string{}, revenue: map[string]string{}, orgs: map[string]bool{}}
	days := []string{}
	for d := dayFrom; d.Before(dayTo); d = d.Add(24 * time.Hour) {
		if present[d.Format("2006-01-02")] {
			days = append(days, d.Format("2006-01-02"))
		}
	}
	if len(days) == 0 {
		return agg, nil
	}
	b := &condBuilder{}
	dateList(b, "d.day", days)
	rollupDimConds(b, p, "d")
	var tx, paid, failed, expired int64
	if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(d.created), 0), COALESCE(SUM(d.succeeded), 0),
		       COALESCE(SUM(d.failed), 0), COALESCE(SUM(d.expired), 0)
		FROM app.analytics_daily_app d WHERE %s`, b.where()), nil, b.args...).Scan(&tx, &paid, &failed, &expired); err != nil {
		return agg, fmt.Errorf("rollup sums: %w", err)
	}
	agg.tx, agg.paid, agg.failed, agg.expired = tx, paid, failed, expired
	mb := &condBuilder{}
	dateList(mb, "d.day", days)
	rollupDimConds(mb, p, "d")
	gross, err := r.queryMoneyByCurrency(ctx, fmt.Sprintf(`
		SELECT d.currency, COALESCE(SUM(d.gross), 0)::text
		FROM app.analytics_daily_app d WHERE %s GROUP BY d.currency`, mb.where()), append([]any{}, mb.args...))
	if err != nil {
		return agg, err
	}
	agg.gross = gross
	rb := &condBuilder{}
	dateList(rb, "m.day", days)
	if len(p.OrgIDs) == 1 {
		rb.conds = append(rb.conds, "m.org_id = "+rb.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		rb.conds = append(rb.conds, "m.org_id = ANY("+rb.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		rb.conds = append(rb.conds, "m.app_id = "+rb.ph(p.AppID)+"::uuid")
	}
	revRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT m.currency,
		       (COALESCE(SUM(m.fees), 0) - COALESCE(SUM(m.fee_reversals), 0))::text
		FROM app.analytics_daily_app_money m WHERE %s GROUP BY m.currency`, rb.where()), nil, rb.args...)
	if err != nil {
		return agg, fmt.Errorf("rollup revenue: %w", err)
	}
	for revRows.Next() {
		var currency, net string
		if err := revRows.Scan(&currency, &net); err != nil {
			revRows.Close()
			return agg, fmt.Errorf("scan rollup revenue: %w", err)
		}
		agg.revenue[currency] = net
	}
	revRows.Close()
	if err := revRows.Err(); err != nil {
		return agg, fmt.Errorf("iterate rollup revenue: %w", err)
	}
	orgRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT DISTINCT d.org_id::text FROM app.analytics_daily_app d WHERE %s AND d.succeeded > 0`, b.where()), nil, b.args...)
	if err != nil {
		return agg, fmt.Errorf("rollup active orgs: %w", err)
	}
	defer orgRows.Close()
	for orgRows.Next() {
		var orgID string
		if err := orgRows.Scan(&orgID); err != nil {
			return agg, fmt.Errorf("scan rollup active orgs: %w", err)
		}
		agg.orgs[orgID] = true
	}
	if err := orgRows.Err(); err != nil {
		return agg, fmt.Errorf("iterate rollup active orgs: %w", err)
	}
	return agg, nil
}

// periodAggRange merges rollup whole-days (with live fallback for missing
// days) and live edges for one window.
func (r *PostgresRepository) periodAggRange(ctx context.Context, p Params, from, to time.Time) (periodSums, error) {
	agg := periodSums{gross: map[string]string{}, revenue: map[string]string{}, orgs: map[string]bool{}}
	wf, wt, hasWhole, hf, ht, tf, tt, hasHead, hasTail := splitRange(from, to)
	merge := func(other periodSums) {
		agg.tx += other.tx
		agg.paid += other.paid
		agg.failed += other.failed
		agg.expired += other.expired
		addMoneyMap(agg.gross, other.gross)
		addMoneyMap(agg.revenue, other.revenue)
		for o := range other.orgs {
			agg.orgs[o] = true
		}
	}
	if hasWhole {
		present, err := r.rollupCoverage(ctx, p, wf, wt)
		if err != nil {
			return agg, err
		}
		for d := wf; d.Before(wt); d = d.Add(24 * time.Hour) {
			if present[d.Format("2006-01-02")] {
				continue
			}
			live, err := r.liveSums(ctx, p, d, d.Add(24*time.Hour))
			if err != nil {
				return agg, err
			}
			merge(live)
		}
		rolled, err := r.rollupSums(ctx, p, wf, wt, present)
		if err != nil {
			return agg, err
		}
		merge(rolled)
	}
	for _, edge := range []struct {
		from, to time.Time
		has      bool
	}{{hf, ht, hasHead}, {tf, tt, hasTail}} {
		if !edge.has {
			continue
		}
		live, err := r.liveSums(ctx, p, edge.from, edge.to)
		if err != nil {
			return agg, err
		}
		merge(live)
	}
	return agg, nil
}

// eatBucketForDay maps an EAT date to the series bucket label.
func eatBucketForDay(day time.Time, granularity string) string {
	loc, err := time.LoadLocation(Timezone)
	if err != nil {
		loc = time.UTC
	}
	day = day.In(loc)
	switch granularity {
	case GranularityWeek:
		monday := day.AddDate(0, 0, -(int(day.Weekday())+6)%7)
		return monday.Format("2006-01-02")
	case GranularityMonth:
		return day.Format("2006-01")
	default:
		return day.Format("2006-01-02")
	}
}

// rollupSeriesDayRows reads daily grains for whole days (present only).
// dayGrain carries raw additive sums so merged rates stay exact.
type dayGrain struct {
	tx, paid, bad int64
	gross         MoneyByCurrency
}

// rollupDayGrains reads daily grains for whole days (present only).
func (r *PostgresRepository) rollupDayGrains(ctx context.Context, p Params, dayFrom, dayTo time.Time, present map[string]bool) (map[string]*dayGrain, error) {
	out := map[string]*dayGrain{}
	days := []string{}
	for d := dayFrom; d.Before(dayTo); d = d.Add(24 * time.Hour) {
		if present[d.Format("2006-01-02")] {
			days = append(days, d.Format("2006-01-02"))
		}
	}
	if len(days) == 0 {
		return out, nil
	}
	b := &condBuilder{}
	dateList(b, "d.day", days)
	rollupDimConds(b, p, "d")
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT d.day::text, COALESCE(SUM(d.created), 0), COALESCE(SUM(d.succeeded), 0),
		       COALESCE(SUM(d.failed), 0), COALESCE(SUM(d.expired), 0)
		FROM app.analytics_daily_app d WHERE %s GROUP BY 1`, b.where()), nil, b.args...)
	if err != nil {
		return out, fmt.Errorf("rollup series: %w", err)
	}
	for rows.Next() {
		var day string
		g := &dayGrain{gross: MoneyByCurrency{}}
		var expired int64
		if err := rows.Scan(&day, &g.tx, &g.paid, &g.bad, &expired); err != nil {
			rows.Close()
			return out, fmt.Errorf("scan rollup series: %w", err)
		}
		g.bad += expired
		out[day] = g
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate rollup series: %w", err)
	}
	gb := &condBuilder{}
	dateList(gb, "d.day", days)
	rollupDimConds(gb, p, "d")
	grows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT d.day::text, d.currency, COALESCE(SUM(d.gross), 0)::text
		FROM app.analytics_daily_app d WHERE %s GROUP BY 1, 2`, gb.where()), nil, gb.args...)
	if err != nil {
		return out, fmt.Errorf("rollup series gross: %w", err)
	}
	for grows.Next() {
		var day, currency, gross string
		if err := grows.Scan(&day, &currency, &gross); err != nil {
			grows.Close()
			return out, fmt.Errorf("scan rollup series gross: %w", err)
		}
		if g, ok := out[day]; ok {
			g.gross[currency] = gross
		}
	}
	grows.Close()
	return out, grows.Err()
}

// liveSeriesGrains runs the single-round-trip live series query over a
// range, returning raw grains keyed by bucket.
func (r *PostgresRepository) liveSeriesGrains(ctx context.Context, p Params, from, to time.Time) (map[string]*dayGrain, error) {
	out := map[string]*dayGrain{}
	sb := &condBuilder{}
	sb.orderRange(from, to)
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
		SELECT bucket, SUM(tx_c), SUM(paid_c), SUM(bad_c), jsonb_object_agg(currency, gross)
		FROM (
			SELECT %s AS bucket, o.currency,
			       COUNT(*) AS tx_c,
			       COUNT(*) FILTER (WHERE o.status = 'paid') AS paid_c,
			       COUNT(*) FILTER (WHERE o.status IN ('failed','cancelled','reversed','expired')) AS bad_c,
			       COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text AS gross
			FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2
		) g GROUP BY bucket`, bucketExpr(p.Granularity, "o.created_at"), join, sb.where()), nil, sb.args...)
	if err != nil {
		return out, fmt.Errorf("live series: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var bucket, grossJSON string
		g := &dayGrain{gross: MoneyByCurrency{}}
		if err := rows.Scan(&bucket, &g.tx, &g.paid, &g.bad, &grossJSON); err != nil {
			return out, fmt.Errorf("scan live series: %w", err)
		}
		if err := json.Unmarshal([]byte(grossJSON), &g.gross); err != nil {
			return out, fmt.Errorf("decode live series gross: %w", err)
		}
		out[bucket] = g
	}
	return out, rows.Err()
}

// periodSeries merges rollup whole-days (+live fallback for missing days)
// and live edges into bucketed points. Week/month re-bucket daily grains
// in Go; hour granularity always reads live (capped to 7 days in params).
func (r *PostgresRepository) periodSeries(ctx context.Context, p Params, from, to time.Time) ([]OverviewPoint, error) {
	merged := map[string]*dayGrain{}
	add := func(bucket string, g *dayGrain) {
		m, ok := merged[bucket]
		if !ok {
			m = &dayGrain{gross: MoneyByCurrency{}}
			merged[bucket] = m
		}
		m.tx += g.tx
		m.paid += g.paid
		m.bad += g.bad
		addMoneyMap(m.gross, g.gross)
	}
	bucketOf := func(day time.Time) string {
		if p.Granularity == GranularityHour {
			return day.Format("2006-01-02 15:04")
		}
		return eatBucketForDay(day, p.Granularity)
	}
	if p.Granularity == GranularityHour {
		live, err := r.liveSeriesGrains(ctx, p, from, to)
		if err != nil {
			return nil, err
		}
		for bucket, g := range live {
			add(bucket, g)
		}
	} else {
		wf, wt, hasWhole, hf, ht, tf, tt, hasHead, hasTail := splitRange(from, to)
		if hasWhole {
			present, err := r.rollupCoverage(ctx, p, wf, wt)
			if err != nil {
				return nil, err
			}
			grains, err := r.rollupDayGrains(ctx, p, wf, wt, present)
			if err != nil {
				return nil, err
			}
			for dayStr, g := range grains {
				day, err := time.Parse("2006-01-02", dayStr)
				if err != nil {
					return nil, fmt.Errorf("parse rollup day: %w", err)
				}
				add(bucketOf(day), g)
			}
			for d := wf; d.Before(wt); d = d.Add(24 * time.Hour) {
				if present[d.Format("2006-01-02")] {
					continue
				}
				live, err := r.liveSeriesGrains(ctx, p, d, d.Add(24*time.Hour))
				if err != nil {
					return nil, err
				}
				for bucket, g := range live {
					add(bucket, g)
				}
			}
		}
		for _, edge := range []struct {
			from, to time.Time
			has      bool
		}{{hf, ht, hasHead}, {tf, tt, hasTail}} {
			if !edge.has {
				continue
			}
			live, err := r.liveSeriesGrains(ctx, p, edge.from, edge.to)
			if err != nil {
				return nil, err
			}
			for bucket, g := range live {
				add(bucket, g)
			}
		}
	}
	points := make([]OverviewPoint, 0, len(merged))
	for bucket, g := range merged {
		pt := OverviewPoint{Bucket: bucket, TxCount: g.tx, GrossByCurrency: g.gross}
		if g.gross == nil {
			pt.GrossByCurrency = MoneyByCurrency{}
		}
		if g.paid+g.bad > 0 {
			pt.SuccessRate = floatPtr(float64(g.paid) / float64(g.paid+g.bad))
		}
		points = append(points, pt)
	}
	sort.Slice(points, func(i, j int) bool { return points[i].Bucket < points[j].Bucket })
	return points, nil
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

// topRevenueRow is one org+currency revenue line for TopMerchants.
type topRevenueRow struct {
	orgID, currency, net string
	refunds, credits     int64
}

// topRevenueFromRollups serves the no-provider-filter case from the money
// rollup (+ daily succeeded counts for refund rate) with a live-today
// tail, so the 1.4M-row ledger scan is skipped.
func (r *PostgresRepository) topRevenueFromRollups(ctx context.Context, p Params) ([]topRevenueRow, error) {
	loc, err := time.LoadLocation(Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	type key struct{ org, currency string }
	net := map[key]float64{}
	refunds := map[string]int64{}
	credits := map[string]int64{}
	add := func(org, currency string, n float64, ref, cred int64) {
		k := key{org, currency}
		net[k] += n
		refunds[org] += ref
		credits[org] += cred
	}
	// Whole past days from rollups.
	eb := &condBuilder{}
	eb.conds = append(eb.conds, "m.day < "+eb.ph(todayStart.Format("2006-01-02"))+"::date")
	eb.conds = append(eb.conds, "m.day >= "+eb.ph(p.From.In(loc).Format("2006-01-02"))+"::date")
	if len(p.OrgIDs) == 1 {
		eb.conds = append(eb.conds, "m.org_id = "+eb.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		eb.conds = append(eb.conds, "m.org_id = ANY("+eb.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		eb.conds = append(eb.conds, "m.app_id = "+eb.ph(p.AppID)+"::uuid")
	}
	erows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT m.org_id::text, m.currency,
		       COALESCE(SUM(m.fees), 0)::text, COALESCE(SUM(m.fee_reversals), 0)::text,
		       COALESCE(SUM(m.refunds), 0)
		FROM app.analytics_daily_app_money m WHERE %s GROUP BY 1, 2`, eb.where()), nil, eb.args...)
	if err != nil {
		return nil, fmt.Errorf("top revenue rollup: %w", err)
	}
	for erows.Next() {
		var org, currency, fees, reversals string
		var ref int64
		if err := erows.Scan(&org, &currency, &fees, &reversals, &ref); err != nil {
			erows.Close()
			return nil, fmt.Errorf("scan top revenue rollup: %w", err)
		}
		add(org, currency, parseMoney(fees)-parseMoney(reversals), ref, 0)
	}
	erows.Close()
	if err := erows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top revenue rollup: %w", err)
	}
	// Succeeded counts (refund-rate denominator) from the daily grain.
	sb := &condBuilder{}
	sb.conds = append(sb.conds, "d.day < "+sb.ph(todayStart.Format("2006-01-02"))+"::date")
	sb.conds = append(sb.conds, "d.day >= "+sb.ph(p.From.In(loc).Format("2006-01-02"))+"::date")
	sb.conds = append(sb.conds, "d.environment = 'live'")
	if len(p.OrgIDs) == 1 {
		sb.conds = append(sb.conds, "d.org_id = "+sb.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		sb.conds = append(sb.conds, "d.org_id = ANY("+sb.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		sb.conds = append(sb.conds, "d.app_id = "+sb.ph(p.AppID)+"::uuid")
	}
	srows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT d.org_id::text, COALESCE(SUM(d.succeeded), 0)
		FROM app.analytics_daily_app d WHERE %s GROUP BY 1`, sb.where()), nil, sb.args...)
	if err != nil {
		return nil, fmt.Errorf("top revenue succeeded: %w", err)
	}
	for srows.Next() {
		var org string
		var n int64
		if err := srows.Scan(&org, &n); err != nil {
			srows.Close()
			return nil, fmt.Errorf("scan top revenue succeeded: %w", err)
		}
		credits[org] += n
	}
	srows.Close()
	if err := srows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top revenue succeeded: %w", err)
	}
	// Live tail: today (and any range head before the first whole day is
	// negligible — ledgers post at settlement, same-day only).
	lb := &condBuilder{}
	lb.conds = append(lb.conds, "l.created_at >= "+lb.ph(todayStart.UTC()), "l.created_at < "+lb.ph(p.To))
	lb.conds = append(lb.conds, "o.environment = 'live'")
	if len(p.OrgIDs) == 1 {
		lb.conds = append(lb.conds, "a.org_id = "+lb.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		lb.conds = append(lb.conds, "a.org_id = ANY("+lb.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		lb.conds = append(lb.conds, "l.app_id = "+lb.ph(p.AppID)+"::uuid")
	}
	lrows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT a.org_id::text, l.currency,
		       (COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)
		        - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0))::text,
		       COUNT(*) FILTER (WHERE l.entry_type = 'refund_debit'),
		       COUNT(*) FILTER (WHERE l.entry_type = 'payment_credit')
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s GROUP BY 1, 2`, lb.where()), nil, lb.args...)
	if err != nil {
		return nil, fmt.Errorf("top revenue live tail: %w", err)
	}
	for lrows.Next() {
		var org, currency, netStr string
		var ref, cred int64
		if err := lrows.Scan(&org, &currency, &netStr, &ref, &cred); err != nil {
			lrows.Close()
			return nil, fmt.Errorf("scan top revenue live tail: %w", err)
		}
		add(org, currency, parseMoney(netStr), ref, cred)
	}
	lrows.Close()
	if err := lrows.Err(); err != nil {
		return nil, fmt.Errorf("iterate top revenue live tail: %w", err)
	}
	out := []topRevenueRow{}
	for k, n := range net {
		out = append(out, topRevenueRow{orgID: k.org, currency: k.currency, net: fmt.Sprintf("%.2f", n), refunds: refunds[k.org], credits: credits[k.org]})
	}
	return out, nil
}

func parseMoney(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
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
	type topBase struct {
		row  MerchantRow
		paid int64
	}
	type topVol struct{ orgID, currency, gross string }
	type topRev struct {
		orgID, currency, net       string
		refunds, credits           int64
	}
	type topTrend struct {
		orgID      string
		curr, prev int64
	}
	var base []topBase
	var vols []topVol
	var revs []topRev
	var rollupRevs []topRevenueRow
	var trends []topTrend
	var topErr error
	var topMu sync.Mutex
	topSetErr := func(err error) {
		if err == nil {
			return
		}
		topMu.Lock()
		if topErr == nil {
			topErr = err
		}
		topMu.Unlock()
	}
	var topWg sync.WaitGroup
	topRun := func(fn func()) {
		topWg.Add(1)
		go func() {
			defer topWg.Done()
			fn()
		}()
	}
	// The four aggregations are independent — run concurrently.
	topRun(func() {
		rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
			SELECT a.org_id::text, org.name, org.kyc_status, COUNT(*), %s,
			       COUNT(*) FILTER (WHERE o.status = 'paid')
			FROM app.payment_orders o
			JOIN app.payment_apps a ON a.id = o.app_id
			JOIN app.organizations org ON org.id = a.org_id
			WHERE %s GROUP BY 1, 2, 3`, successRateSQL, where), nil, args...)
		if err != nil {
			topSetErr(fmt.Errorf("top merchants: %w", err))
			return
		}
		defer rows.Close()
		var out []topBase
		for rows.Next() {
			var b topBase
			var paid int64
			if err := rows.Scan(&b.row.OrgID, &b.row.OrgName, &b.row.KYCStatus, &b.row.TxCount, &b.row.SuccessRate, &paid); err != nil {
				topSetErr(fmt.Errorf("scan top merchants: %w", err))
				return
			}
			b.row.TPVByCurrency = MoneyByCurrency{}
			b.row.RevenueByCurrency = MoneyByCurrency{}
			b.paid = paid
			out = append(out, b)
		}
		if err := rows.Err(); err != nil {
			topSetErr(fmt.Errorf("iterate top merchants: %w", err))
			return
		}
		topMu.Lock()
		base = out
		topMu.Unlock()
	})
	topRun(func() {
		volRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
			SELECT a.org_id::text, o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
			FROM app.payment_orders o
			JOIN app.payment_apps a ON a.id = o.app_id
			WHERE %s GROUP BY 1, 2`, where), nil, args...)
		if err != nil {
			topSetErr(fmt.Errorf("top merchant volume: %w", err))
			return
		}
		defer volRows.Close()
		var out []topVol
		for volRows.Next() {
			var v topVol
			if err := volRows.Scan(&v.orgID, &v.currency, &v.gross); err != nil {
				topSetErr(fmt.Errorf("scan top merchant volume: %w", err))
				return
			}
			out = append(out, v)
		}
		if err := volRows.Err(); err != nil {
			topSetErr(fmt.Errorf("iterate top merchant volume: %w", err))
			return
		}
		topMu.Lock()
		vols = out
		topMu.Unlock()
	})
	topRun(func() {
		// Revenue from the money rollup (tiny) unless a provider filter
		// forces the live ledger path (money grain has no provider dim).
		if p.Provider == "" {
			out, err := r.topRevenueFromRollups(ctx, p)
			if err != nil {
				topSetErr(err)
				return
			}
			topMu.Lock()
			rollupRevs = out
			topMu.Unlock()
			return
		}
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
			topSetErr(fmt.Errorf("top merchant revenue: %w", err))
			return
		}
		defer revRows.Close()
		var out []topRev
		for revRows.Next() {
			var v topRev
			if err := revRows.Scan(&v.orgID, &v.currency, &v.net, &v.refunds, &v.credits); err != nil {
				topSetErr(fmt.Errorf("scan top merchant revenue: %w", err))
				return
			}
			out = append(out, v)
		}
		if err := revRows.Err(); err != nil {
			topSetErr(fmt.Errorf("iterate top merchant revenue: %w", err))
			return
		}
		topMu.Lock()
		revs = out
		topMu.Unlock()
	})
	topRun(func() {
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
			topSetErr(fmt.Errorf("top merchant trend: %w", err))
			return
		}
		defer trendRows.Close()
		var out []topTrend
		for trendRows.Next() {
			var v topTrend
			if err := trendRows.Scan(&v.orgID, &v.curr, &v.prev); err != nil {
				topSetErr(fmt.Errorf("scan top merchant trend: %w", err))
				return
			}
			out = append(out, v)
		}
		if err := trendRows.Err(); err != nil {
			topSetErr(fmt.Errorf("iterate top merchant trend: %w", err))
			return
		}
		topMu.Lock()
		trends = out
		topMu.Unlock()
	})
	topWg.Wait()
	if topErr != nil {
		return MerchantList{}, topErr
	}
	byOrg := map[string]*MerchantRow{}
	order := []string{}
	for _, b := range base {
		row := b.row
		byOrg[row.OrgID] = &row
		order = append(order, row.OrgID)
		_ = b.paid
	}
	for _, v := range vols {
		if row, ok := byOrg[v.orgID]; ok {
			row.TPVByCurrency[v.currency] = v.gross
		}
	}
	for _, v := range rollupRevs {
		if row, ok := byOrg[v.orgID]; ok {
			row.RevenueByCurrency[v.currency] = v.net
			if v.credits > 0 {
				rate := float64(v.refunds) / float64(v.credits)
				if row.RefundRate == nil {
					row.RefundRate = &rate
				} else {
					combined := (*row.RefundRate + rate) / 2
					row.RefundRate = &combined
				}
			}
		}
	}
	for _, v := range revs {
		if row, ok := byOrg[v.orgID]; ok {
			row.RevenueByCurrency[v.currency] = v.net
			if v.credits > 0 {
				rate := float64(v.refunds) / float64(v.credits)
				if row.RefundRate == nil {
					row.RefundRate = &rate
				} else {
					combined := (*row.RefundRate + rate) / 2
					row.RefundRate = &combined
				}
			}
		}
	}
	for _, v := range trends {
		if row, ok := byOrg[v.orgID]; ok {
			row.TrendPct = pctChange(float64(v.curr), float64(v.prev))
		}
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
