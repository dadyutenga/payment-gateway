package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// FailureBreakdown ranks normalized failure reasons with affected orgs
// and sample order ids.
func (r *PostgresRepository) FailureBreakdown(ctx context.Context, p Params) ([]FailureRow, error) {
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.orderDims(p.Provider, "")
	joinApps := b.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
	}
	b.conds = append(b.conds, "o.status IN ('failed','cancelled','expired')")
	join := ""
	if joinApps {
		join = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT COALESCE(NULLIF(o.failure_code, ''), 'unspecified'), o.provider,
		       COUNT(*),
		       COUNT(DISTINCT a2.org_id)
		FROM app.payment_orders o %s
		LEFT JOIN app.payment_apps a2 ON a2.id = o.app_id
		WHERE %s AND o.status IN ('failed','cancelled','expired') GROUP BY 1, 2 ORDER BY COUNT(*) DESC LIMIT 100`, join, b.where()), nil, b.args...)
	if err != nil {
		return nil, fmt.Errorf("failure breakdown: %w", err)
	}
	defer rows.Close()
	type key struct{ code, provider string }
	out := []FailureRow{}
	keys := []key{}
	seen := map[key]int{}
	for rows.Next() {
		var row FailureRow
		if err := rows.Scan(&row.Code, &row.Provider, &row.Count, &row.AffectedOrgs); err != nil {
			return nil, fmt.Errorf("scan failure breakdown: %w", err)
		}
		row.SampleOrderIDs = []string{}
		seen[key{row.Code, row.Provider}] = len(out)
		keys = append(keys, key{row.Code, row.Provider})
		out = append(out, row)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate failure breakdown: %w", err)
	}
	// Sample order ids for the top rows (separate bounded queries keep the
	// main aggregation cheap).
	for i, k := range keys {
		if i >= 20 {
			break
		}
		sb := &condBuilder{}
		sb.orderRange(p.From, p.To)
		sb.orderDims(p.Provider, "")
		sjoinApps := sb.orgScope(p.OrgIDs, p.AppID, "a.org_id")
		if p.AppID != "" {
			sb.conds = append(sb.conds, "o.app_id = "+sb.ph(p.AppID)+"::uuid")
		}
		sb.conds = append(sb.conds, "o.status IN ('failed','cancelled','expired')")
		sb.conds = append(sb.conds, "COALESCE(NULLIF(o.failure_code, ''), 'unspecified') = "+sb.ph(k.code))
		sb.conds = append(sb.conds, "o.provider = "+sb.ph(k.provider))
		sjoin := ""
		if sjoinApps {
			sjoin = "JOIN app.payment_apps a ON a.id = o.app_id"
		}
		srows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
			SELECT o.id::text FROM app.payment_orders o %s WHERE %s
			ORDER BY o.created_at DESC LIMIT 5`, sjoin, sb.where()), nil, sb.args...)
		if err != nil {
			return nil, fmt.Errorf("failure samples: %w", err)
		}
		var samples []string
		for srows.Next() {
			var id string
			if err := srows.Scan(&id); err != nil {
				srows.Close()
				return nil, fmt.Errorf("scan failure samples: %w", err)
			}
			samples = append(samples, id)
		}
		srows.Close()
		if err := srows.Err(); err != nil {
			return nil, fmt.Errorf("iterate failure samples: %w", err)
		}
		out[seen[k]].SampleOrderIDs = samples
	}
	return out, nil
}

// WithdrawalStats covers pending amounts, aging buckets, failed payouts,
// the approval queue, and average time to payout.
func (r *PostgresRepository) WithdrawalStats(ctx context.Context, p Params) (WithdrawalStats, error) {
	out := WithdrawalStats{Timezone: Timezone, Aging: []AgingBucket{}}
	now := time.Now().UTC()
	b := &condBuilder{}
	joinApps := len(p.OrgIDs) > 0
	join := ""
	if joinApps {
		join = "JOIN app.payment_apps a ON a.id = w.app_id"
		if len(p.OrgIDs) == 1 {
			b.conds = append(b.conds, "a.org_id = "+b.ph(p.OrgIDs[0])+"::uuid")
		} else {
			b.conds = append(b.conds, "a.org_id = ANY("+b.ph(p.OrgIDs)+"::uuid[])")
		}
	}
	where := b.where()
	var pending, queue, failed int64
	if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT COUNT(*) FILTER (WHERE w.status IN ('requested','approved','processing')),
		       COUNT(*) FILTER (WHERE w.status = 'requested'),
		       COUNT(*) FILTER (WHERE w.status = 'failed')
		FROM app.payment_withdrawals w %s WHERE %s`, join, where), nil, b.args...).Scan(&pending, &queue, &failed); err != nil {
		return out, fmt.Errorf("withdrawal counts: %w", err)
	}
	out.PendingCount, out.ApprovalQueue, out.FailedPayouts = pending, queue, failed
	out.PendingByCurrency = MoneyByCurrency{}
	amtRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT w.currency, COALESCE(SUM(w.amount), 0)::text
		FROM app.payment_withdrawals w %s WHERE %s AND w.status IN ('requested','approved','processing')
		GROUP BY 1`, join, where), nil, b.args...)
	if err != nil {
		return out, fmt.Errorf("withdrawal pending amounts: %w", err)
	}
	for amtRows.Next() {
		var currency, amount string
		if err := amtRows.Scan(&currency, &amount); err != nil {
			amtRows.Close()
			return out, fmt.Errorf("scan withdrawal pending amounts: %w", err)
		}
		out.PendingByCurrency[currency] = amount
	}
	amtRows.Close()
	if err := amtRows.Err(); err != nil {
		return out, fmt.Errorf("iterate withdrawal pending amounts: %w", err)
	}
	// Aging over open withdrawals (requested/approved/processing).
	buckets := []struct {
		label    string
		minHours float64
		maxHours float64 // -1 = open-ended
	}{
		{"<1h", 0, 1}, {"1-6h", 1, 6}, {"6-24h", 6, 24}, {"1-3d", 24, 72}, {">3d", 72, -1},
	}
	for _, bk := range buckets {
		age := "EXTRACT(EPOCH FROM ($1 - w.created_at))/3600"
		cond := age + " >= " + fmt.Sprintf("%f", bk.minHours)
		if bk.maxHours >= 0 {
			cond += " AND " + age + " < " + fmt.Sprintf("%f", bk.maxHours)
		}
		var count int64
		if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
			SELECT COUNT(*) FROM app.payment_withdrawals w %s
			WHERE %s AND w.status IN ('requested','approved','processing') AND %s`, join, where, cond),
			nil, append(append([]any{}, b.args...), now)...).Scan(&count); err != nil {
			return out, fmt.Errorf("withdrawal aging: %w", err)
		}
		ab := AgingBucket{Bucket: bk.label, Count: count, ByCurrency: MoneyByCurrency{}}
		if count > 0 {
			crows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
				SELECT w.currency, COALESCE(SUM(w.amount), 0)::text
				FROM app.payment_withdrawals w %s
				WHERE %s AND w.status IN ('requested','approved','processing') AND %s GROUP BY 1`, join, where, cond),
				nil, append(append([]any{}, b.args...), now)...)
			if err != nil {
				return out, fmt.Errorf("withdrawal aging amounts: %w", err)
			}
			for crows.Next() {
				var currency, amount string
				if err := crows.Scan(&currency, &amount); err != nil {
					crows.Close()
					return out, fmt.Errorf("scan withdrawal aging amounts: %w", err)
				}
				ab.ByCurrency[currency] = amount
			}
			crows.Close()
			if err := crows.Err(); err != nil {
				return out, fmt.Errorf("iterate withdrawal aging amounts: %w", err)
			}
		}
		out.Aging = append(out.Aging, ab)
	}
	// Average requested → paid hours (updated_at proxies the paid moment).
	var avg *float64
	if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT AVG(EXTRACT(EPOCH FROM (w.updated_at - w.created_at))/3600)
		FROM app.payment_withdrawals w %s WHERE %s AND w.status = 'paid'`, join, where), nil, b.args...).Scan(&avg); err != nil {
		return out, fmt.Errorf("withdrawal avg payout: %w", err)
	}
	out.AvgPayoutHours = avg
	return out, nil
}

// WebhookHealth covers delivery success/retry rates, p95 latency, stuck
// processing rows, and repeat offender endpoints.
func (r *PostgresRepository) WebhookHealth(ctx context.Context, p Params) (WebhookHealth, error) {
	out := WebhookHealth{Timezone: Timezone, Offenders: []WebhookOffender{}}
	stuckCutoff := time.Now().UTC().Add(-time.Duration(p.StuckMinutes) * time.Minute)
	var total, delivered, retried int64
	var p95 *float64
	var stuck int64
	if err := r.db.QueryRowEx(ctx, `
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE status = 'delivered'),
		       COUNT(*) FILTER (WHERE status = 'retrying'),
		       percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (delivered_at - created_at))) FILTER (WHERE status = 'delivered' AND delivered_at IS NOT NULL),
		       COUNT(*) FILTER (WHERE status = 'processing' AND COALESCE(last_attempt_at, created_at) < $1)
		FROM app.payment_webhook_deliveries`, nil, stuckCutoff).Scan(&total, &delivered, &retried, &p95, &stuck); err != nil {
		return out, fmt.Errorf("webhook health: %w", err)
	}
	if total > 0 {
		sr := float64(delivered) / float64(total)
		rr := float64(retried) / float64(total)
		out.SuccessRate, out.RetryRate = &sr, &rr
	}
	out.P95LatencyS, out.StuckProcessing = p95, stuck
	rows, err := r.db.QueryEx(ctx, `
		SELECT e.id::text, e.url, e.app_id::text,
		       COUNT(*) FILTER (WHERE d.status = 'failed') AS failed,
		       COALESCE(MAX(d.last_error) FILTER (WHERE d.status = 'failed'), '')
		FROM app.payment_webhook_endpoints e
		JOIN app.payment_webhook_deliveries d ON d.endpoint_id = e.id
		GROUP BY 1, 2, 3 HAVING COUNT(*) FILTER (WHERE d.status = 'failed') > 0
		ORDER BY failed DESC LIMIT 10`, nil)
	if err != nil {
		return out, fmt.Errorf("webhook offenders: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var o WebhookOffender
		if err := rows.Scan(&o.EndpointID, &o.URL, &o.AppID, &o.Failed, &o.LastError); err != nil {
			return out, fmt.Errorf("scan webhook offenders: %w", err)
		}
		out.Offenders = append(out.Offenders, o)
	}
	return out, rows.Err()
}

// StuckOrders lists pending/processing orders older than N minutes.
func (r *PostgresRepository) StuckOrders(ctx context.Context, p Params) ([]StuckOrder, error) {
	cutoff := time.Now().UTC().Add(-time.Duration(p.StuckMinutes) * time.Minute)
	b := &condBuilder{}
	b.conds = append(b.conds, "o.status IN ('pending','processing')", "o.updated_at < "+b.ph(cutoff))
	b.orderDims(p.Provider, "")
	if len(p.OrgIDs) == 1 {
		b.conds = append(b.conds, "a.org_id = "+b.ph(p.OrgIDs[0])+"::uuid")
	} else if len(p.OrgIDs) > 1 {
		b.conds = append(b.conds, "a.org_id = ANY("+b.ph(p.OrgIDs)+"::uuid[])")
	}
	if p.AppID != "" {
		b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.id::text, COALESCE(o.app_id::text, ''), COALESCE(a.org_id::text, ''), COALESCE(org.name, ''),
		       o.provider, o.status, o.amount::text, o.currency,
		       EXTRACT(EPOCH FROM ($%d - o.updated_at))/60, o.updated_at
		FROM app.payment_orders o
		LEFT JOIN app.payment_apps a ON a.id = o.app_id
		LEFT JOIN app.organizations org ON org.id = a.org_id
		WHERE %s ORDER BY o.updated_at ASC LIMIT 200`, len(b.args)+1, b.where()), nil, append(b.args, time.Now().UTC())...)
	if err != nil {
		return nil, fmt.Errorf("stuck orders: %w", err)
	}
	defer rows.Close()
	out := []StuckOrder{}
	for rows.Next() {
		var s StuckOrder
		if err := rows.Scan(&s.OrderID, &s.AppID, &s.OrgID, &s.OrgName, &s.Provider, &s.Status, &s.Amount, &s.Currency, &s.AgeMinutes, &s.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan stuck orders: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Unreconciled finds paid-without-ledger-credit and
// ledger-credit-without-paid-order mismatches (capped).
func (r *PostgresRepository) Unreconciled(ctx context.Context, p Params, limit int) ([]UnreconciledItem, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	out := []UnreconciledItem{}
	paidRows, err := r.db.QueryEx(ctx, `
		SELECT o.id::text, COALESCE(o.app_id::text, ''), COALESCE(a.org_id::text, ''), o.amount::text, o.currency,
		       EXTRACT(EPOCH FROM (NOW() - o.updated_at))/3600
		FROM app.payment_orders o
		LEFT JOIN app.payment_apps a ON a.id = o.app_id
		LEFT JOIN app.payment_ledger_entries l ON l.payment_order_id = o.id AND l.entry_type = 'payment_credit'
		WHERE o.status = 'paid' AND o.environment = 'live' AND l.id IS NULL
		ORDER BY o.updated_at ASC LIMIT $1`, nil, limit)
	if err != nil {
		return nil, fmt.Errorf("unreconciled paid: %w", err)
	}
	for paidRows.Next() {
		var u UnreconciledItem
		u.Kind = "paid_without_ledger"
		if err := paidRows.Scan(&u.OrderID, &u.AppID, &u.OrgID, &u.Amount, &u.Currency, &u.AgeHours); err != nil {
			paidRows.Close()
			return nil, fmt.Errorf("scan unreconciled paid: %w", err)
		}
		out = append(out, u)
	}
	paidRows.Close()
	if err := paidRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unreconciled paid: %w", err)
	}
	creditRows, err := r.db.QueryEx(ctx, `
		SELECT l.payment_order_id::text, COALESCE(l.app_id::text, ''), COALESCE(a.org_id::text, ''),
		       l.amount::text, l.currency, EXTRACT(EPOCH FROM (NOW() - l.created_at))/3600
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		LEFT JOIN app.payment_apps a ON a.id = l.app_id
		WHERE l.entry_type = 'payment_credit' AND o.status != 'paid'
		ORDER BY l.created_at ASC LIMIT $1`, nil, limit)
	if err != nil {
		return nil, fmt.Errorf("unreconciled credits: %w", err)
	}
	for creditRows.Next() {
		var u UnreconciledItem
		u.Kind = "ledger_without_paid_order"
		if err := creditRows.Scan(&u.OrderID, &u.AppID, &u.OrgID, &u.Amount, &u.Currency, &u.AgeHours); err != nil {
			creditRows.Close()
			return nil, fmt.Errorf("scan unreconciled credits: %w", err)
		}
		out = append(out, u)
	}
	creditRows.Close()
	if err := creditRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate unreconciled credits: %w", err)
	}
	return out, nil
}

// NegativeBalances lists apps whose ledger-derived available balance is
// below zero, with per-currency balances.
func (r *PostgresRepository) NegativeBalances(ctx context.Context) ([]NegativeBalance, error) {
	rows, err := r.db.QueryEx(ctx, `
		SELECT a.id::text, a.name, a.org_id::text, COALESCE(org.name, ''), b.currency, b.balance::text
		FROM (
			SELECT l.app_id, l.currency,
			       (COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'credit'), 0)
			        - COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'debit'), 0)) AS balance
			FROM app.payment_ledger_entries l GROUP BY 1, 2
			HAVING (COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'credit'), 0)
			        - COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'debit'), 0)) < 0
		) b
		JOIN app.payment_apps a ON a.id = b.app_id
		LEFT JOIN app.organizations org ON org.id = a.org_id
		ORDER BY b.balance ASC`, nil)
	if err != nil {
		return nil, fmt.Errorf("negative balances: %w", err)
	}
	defer rows.Close()
	out := []NegativeBalance{}
	for rows.Next() {
		var n NegativeBalance
		if err := rows.Scan(&n.AppID, &n.AppName, &n.OrgID, &n.OrgName, &n.Currency, &n.Balance); err != nil {
			return nil, fmt.Errorf("scan negative balances: %w", err)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// LogExport records analytics exports (reads are not audited).
func (r *PostgresRepository) LogExport(ctx context.Context, actorID, actorEmail, report, ip string) error {
	_, err := r.db.ExecEx(ctx, `INSERT INTO app.audit_log (actor_id, actor_email, action, target_type, target_id, ip)
		VALUES ($1, $2, 'analytics.export', 'analytics_report', $3, $4)`,
		nil, actorID, actorEmail, report, strings.TrimSpace(ip))
	if err != nil {
		return fmt.Errorf("log analytics export: %w", err)
	}
	return nil
}
