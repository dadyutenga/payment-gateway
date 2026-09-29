package analytics

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Merchant overview types -------------------------------------------------

type MerchantOverview struct {
	Overview
	AvgOrderValue MoneyByCurrency `json:"avg_order_value"`
}

type ChannelRow struct {
	Channel    string `json:"channel"`
	TxCount    int64  `json:"tx_count"`
	VolumeByCurrency MoneyByCurrency `json:"volume_by_currency"`
	SuccessRate *float64 `json:"success_rate"`
}

type PeakCell struct {
	DOW    int   `json:"dow"`
	Hour   int   `json:"hour"`
	Tx     int64 `json:"tx_count"`
	Paid   int64 `json:"paid_count"`
}

type PeakHours struct {
	Timezone  string     `json:"timezone"`
	Cells     []PeakCell `json:"cells"`
	BestLabel string     `json:"best_label"`
	WorstLabel string    `json:"worst_label"`
}

type CustomerStats struct {
	Timezone          string           `json:"timezone"`
	RepeatRate        *float64         `json:"repeat_rate"`
	PayersTotal       int64            `json:"payers_total"`
	PayersRepeat      int64            `json:"payers_repeat"`
	Series            []CustomerPoint  `json:"series"`
	TopPayers         []TopPayer       `json:"top_payers"`
	PayerDetailHidden bool             `json:"payer_detail_hidden"`
}

type CustomerPoint struct {
	Bucket    string `json:"bucket"`
	NewPayers int64  `json:"new_payers"`
	Returning int64  `json:"returning_payers"`
}

type TopPayer struct {
	MaskedID string `json:"masked_id"`
	TxCount  int64  `json:"tx_count"`
	Volume   string `json:"volume"`
	Currency string `json:"currency"`
	LastTxn  string `json:"last_txn_at"`
}

type MerchantAppRow struct {
	AppID             string           `json:"app_id"`
	Name              string           `json:"name"`
	Status            string           `json:"status"`
	TPVByCurrency     MoneyByCurrency  `json:"tpv_by_currency"`
	RevenueByCurrency MoneyByCurrency  `json:"revenue_by_currency"`
	TxCount           int64            `json:"tx_count"`
	SuccessRate       *float64         `json:"success_rate"`
	RefundRate        *float64         `json:"refund_rate"`
}

type Settlement struct {
	StatementID string             `json:"statement_id"`
	OrgID       string             `json:"org_id"`
	OrgName     string             `json:"org_name"`
	BusinessName string            `json:"business_name"`
	From        time.Time          `json:"from"`
	To          time.Time          `json:"to"`
	GeneratedAt string             `json:"generated_at"`
	Timezone    string             `json:"timezone"`
	Blocks      []SettlementBlock  `json:"blocks"`
	Truncated   bool               `json:"truncated"`
}

type SettlementBlock struct {
	Currency      string              `json:"currency"`
	Gross         string              `json:"gross"`
	Fees          string              `json:"fees"`
	Refunds       string              `json:"refunds"`
	FeeReversals  string              `json:"fee_reversals"`
	NetSettled    string              `json:"net_settled"`
	OpeningBalance string             `json:"opening_balance"`
	ClosingBalance string             `json:"closing_balance"`
	ByProvider    []SettlementProvider `json:"by_provider"`
	Entries       []SettlementEntry   `json:"entries"`
}

type SettlementProvider struct {
	Provider string `json:"provider"`
	Gross    string `json:"gross"`
	Fees     string `json:"fees"`
	Count    int64  `json:"count"`
}

type SettlementEntry struct {
	ID        string `json:"id"`
	Type      string `json:"entry_type"`
	Direction string `json:"direction"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	OrderID   string `json:"order_id,omitempty"`
	Description string `json:"description"`
	CreatedAt string `json:"created_at"`
}

// scoped clones Params pinned to one org (+ optional app).
func scoped(p Params, orgID, appID string) Params {
	p.OrgID = orgID
	p.OrgIDs = []string{orgID}
	p.AppID = appID
	return p
}

// MerchantAvgOrderValue returns paid gross / paid count per currency.
func (r *PostgresRepository) MerchantAvgOrderValue(ctx context.Context, p Params, env string) (MoneyByCurrency, error) {
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.conds = append(b.conds, "o.environment = "+b.ph(env))
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
	return r.queryMoneyByCurrency(ctx, fmt.Sprintf(`
		SELECT o.currency,
		       (COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)
		        / NULLIF(COUNT(*) FILTER (WHERE o.status = 'paid'), 0))::text
		FROM app.payment_orders o %s WHERE %s GROUP BY o.currency`, join, b.where()), b.args)
}

// MerchantChannels returns the payment-method mix for the scope.
func (r *PostgresRepository) MerchantChannels(ctx context.Context, p Params, env string) ([]ChannelRow, error) {
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.conds = append(b.conds, "o.environment = "+b.ph(env))
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
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT COALESCE(NULLIF(o.channel, ''), 'OTHER'), COUNT(*), %s
		FROM app.payment_orders o %s WHERE %s GROUP BY 1 ORDER BY COUNT(*) DESC`, successRateSQL, join, b.where()), nil, b.args...)
	if err != nil {
		return nil, fmt.Errorf("merchant channels: %w", err)
	}
	defer rows.Close()
	out := []ChannelRow{}
	for rows.Next() {
		var row ChannelRow
		if err := rows.Scan(&row.Channel, &row.TxCount, &row.SuccessRate); err != nil {
			return nil, fmt.Errorf("scan merchant channels: %w", err)
		}
		row.VolumeByCurrency = MoneyByCurrency{}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant channels: %w", err)
	}
	volRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT COALESCE(NULLIF(o.channel, ''), 'OTHER'), o.currency,
		       COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2`, join, b.where()), nil, b.args...)
	if err != nil {
		return nil, fmt.Errorf("merchant channel volume: %w", err)
	}
	byChannel := map[string]*ChannelRow{}
	for i := range out {
		byChannel[out[i].Channel] = &out[i]
	}
	for volRows.Next() {
		var channel, currency, gross string
		if err := volRows.Scan(&channel, &currency, &gross); err != nil {
			volRows.Close()
			return nil, fmt.Errorf("scan merchant channel volume: %w", err)
		}
		if row, ok := byChannel[channel]; ok {
			row.VolumeByCurrency[currency] = gross
		}
	}
	volRows.Close()
	return out, volRows.Err()
}

// MerchantPeakHours returns weekday x hour cells in EAT.
func (r *PostgresRepository) MerchantPeakHours(ctx context.Context, p Params, env string) (PeakHours, error) {
	out := PeakHours{Timezone: Timezone, Cells: []PeakCell{}}
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.conds = append(b.conds, "o.environment = "+b.ph(env))
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
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT EXTRACT(DOW FROM o.created_at AT TIME ZONE '%s')::int,
		       EXTRACT(HOUR FROM o.created_at AT TIME ZONE '%s')::int,
		       COUNT(*), COUNT(*) FILTER (WHERE o.status = 'paid')
		FROM app.payment_orders o %s WHERE %s GROUP BY 1, 2`, Timezone, Timezone, join, b.where()), nil, b.args...)
	if err != nil {
		return out, fmt.Errorf("merchant peak hours: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cell PeakCell
		if err := rows.Scan(&cell.DOW, &cell.Hour, &cell.Tx, &cell.Paid); err != nil {
			return out, fmt.Errorf("scan merchant peak hours: %w", err)
		}
		out.Cells = append(out.Cells, cell)
	}
	if err := rows.Err(); err != nil {
		return out, fmt.Errorf("iterate merchant peak hours: %w", err)
	}
	best, worst := -1, -1
	var bestCell, worstCell PeakCell
	for i, c := range out.Cells {
		if best < 0 || c.Paid > out.Cells[best].Paid {
			best, bestCell = i, c
		}
		if c.Tx > 0 && (worst < 0 || c.Paid < out.Cells[worst].Paid) {
			worst, worstCell = i, c
		}
	}
	days := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	if best >= 0 {
		out.BestLabel = fmt.Sprintf("Most payments arrive %02d:00–%02d:00 on %ss.", bestCell.Hour, (bestCell.Hour+1)%24, days[bestCell.DOW])
	}
	if worst >= 0 {
		out.WorstLabel = fmt.Sprintf("Quietest active hour is %02d:00 on %ss.", worstCell.Hour, days[worstCell.DOW])
	}
	return out, nil
}

// MerchantCustomers computes repeat rate, new-vs-returning series, and
// masked top payers. showDetail=false (developers) omits the payer table.
func (r *PostgresRepository) MerchantCustomers(ctx context.Context, p Params, env string, showDetail bool, mask func(string) string) (CustomerStats, error) {
	out := CustomerStats{Timezone: Timezone, Series: []CustomerPoint{}, TopPayers: []TopPayer{}}
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.conds = append(b.conds, "o.environment = "+b.ph(env))
	b.conds = append(b.conds, "o.status = 'paid'", "o.payer_hash IS NOT NULL")
	joinApps := b.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		b.conds = append(b.conds, "o.app_id = "+b.ph(p.AppID)+"::uuid")
	}
	join := ""
	if joinApps {
		join = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	where, args := b.where(), b.args
	var total, repeat int64
	if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT COUNT(*), COUNT(*) FILTER (WHERE t.cnt > 1)
		FROM (SELECT o.payer_hash AS h, COUNT(*) AS cnt
		      FROM app.payment_orders o %s WHERE %s GROUP BY 1) t`, join, where), nil,
		args...).Scan(&total, &repeat); err != nil {
		return out, fmt.Errorf("customer repeat rate: %w", err)
	}
	out.PayersTotal, out.PayersRepeat = total, repeat
	if total > 0 {
		rate := float64(repeat) / float64(total)
		out.RepeatRate = &rate
	}
	// New vs returning per EAT day bucket: a payer is new in the bucket
	// holding their first-ever paid order.
	fb := &condBuilder{}
	fb.conds = append(fb.conds, "o.environment = "+fb.ph(env))
	fb.conds = append(fb.conds, "o.status = 'paid'", "o.payer_hash IS NOT NULL")
	fbJoinApps := fb.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		fb.conds = append(fb.conds, "o.app_id = "+fb.ph(p.AppID)+"::uuid")
	}
	fbJoin := ""
	if fbJoinApps {
		fbJoin = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	fbWhere, fbArgs := fb.where(), fb.args
	ob := &condBuilder{offset: len(fbArgs)}
	ob.orderRange(p.From, p.To)
	ob.conds = append(ob.conds, "o.environment = "+ob.ph(env))
	ob.conds = append(ob.conds, "o.status = 'paid'", "o.payer_hash IS NOT NULL")
	obJoinApps := ob.orgScope(p.OrgIDs, p.AppID, "a.org_id")
	if p.AppID != "" {
		ob.conds = append(ob.conds, "o.app_id = "+ob.ph(p.AppID)+"::uuid")
	}
	obJoin := ""
	if obJoinApps {
		obJoin = "JOIN app.payment_apps a ON a.id = o.app_id"
	}
	obWhere, obArgs := ob.where(), ob.args
	lo, hi := len(fbArgs)+len(obArgs)+1, len(fbArgs)+len(obArgs)+2
	seriesRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		WITH firsts AS (
			SELECT o.payer_hash AS h, MIN(o.created_at) AS first_at
			FROM app.payment_orders o %s WHERE %s
			GROUP BY 1
		)
		SELECT %s AS bucket,
		       COUNT(DISTINCT CASE WHEN firsts.first_at >= $%d AND firsts.first_at < $%d THEN o.payer_hash END),
		       COUNT(DISTINCT CASE WHEN firsts.first_at < $%d THEN o.payer_hash END)
		FROM app.payment_orders o %s
		JOIN firsts ON firsts.h = o.payer_hash
		WHERE %s GROUP BY 1 ORDER BY MIN(o.created_at)`,
		fbJoin, fbWhere, bucketExpr(p.Granularity, "o.created_at"),
		lo, hi, lo, obJoin, obWhere),
		nil, append(append(append([]any{}, fbArgs...), obArgs...), p.From, p.To)...)
	if err != nil {
		return out, fmt.Errorf("customer series: %w", err)
	}
	defer seriesRows.Close()
	for seriesRows.Next() {
		var pt CustomerPoint
		if err := seriesRows.Scan(&pt.Bucket, &pt.NewPayers, &pt.Returning); err != nil {
			return out, fmt.Errorf("scan customer series: %w", err)
		}
		out.Series = append(out.Series, pt)
	}
	if err := seriesRows.Err(); err != nil {
		return out, fmt.Errorf("iterate customer series: %w", err)
	}
	out.PayerDetailHidden = !showDetail
	if !showDetail {
		return out, nil
	}
	topRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT MAX(o.buyer_phone), COUNT(*), o.currency,
		       COALESCE(SUM(o.amount), 0)::text,
		       to_char(MAX(o.created_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
		FROM app.payment_orders o %s WHERE %s
		GROUP BY o.payer_hash, o.currency ORDER BY COUNT(*) DESC LIMIT 20`, join, where), nil, args...)
	if err != nil {
		return out, fmt.Errorf("top payers: %w", err)
	}
	defer topRows.Close()
	for topRows.Next() {
		var phone, volume, lastTxn string
		var count int64
		var currency string
		if err := topRows.Scan(&phone, &count, &currency, &volume, &lastTxn); err != nil {
			return out, fmt.Errorf("scan top payers: %w", err)
		}
		out.TopPayers = append(out.TopPayers, TopPayer{
			MaskedID: mask(phone), TxCount: count, Volume: volume, Currency: currency, LastTxn: lastTxn,
		})
	}
	return out, topRows.Err()
}

// MerchantAppsTable compares the org's apps (all apps, even idle ones).
func (r *PostgresRepository) MerchantAppsTable(ctx context.Context, p Params, env string) ([]MerchantAppRow, error) {
	// Single builder, strict order: $1 from, $2 to, $3 env, [$4 provider,]
	// [$5 org, [$6 app]]. Env/provider live in the JOIN … ON clause (a
	// WHERE on o.* would drop idle apps); org/app filter the apps table.
	// Org/app placeholder VALUES are reused by number — no duplicates.
	b := &condBuilder{}
	b.orderRange(p.From, p.To)
	b.conds = append(b.conds, "o.environment = "+b.ph(env))
	on := "o.app_id = a.id AND o.created_at >= $1 AND o.created_at < $2 AND o.environment = $3"
	where := "a.status != 'deleted'"
	if p.Provider != "" {
		ph := b.ph(p.Provider)
		on += " AND o.provider = " + ph
	}
	if len(p.OrgIDs) == 1 {
		where += " AND a.org_id = " + b.ph(p.OrgIDs[0]) + "::uuid"
	} else if len(p.OrgIDs) > 1 {
		where += " AND a.org_id = ANY(" + b.ph(p.OrgIDs) + "::uuid[])"
	}
	if p.AppID != "" {
		where += " AND a.id = " + b.ph(p.AppID) + "::uuid"
	}
	rows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT a.id::text, a.name, a.status, COUNT(o.id),
		       %s,
		       COUNT(*) FILTER (WHERE o.status = 'paid'),
		       COUNT(*) FILTER (WHERE EXISTS (
		         SELECT 1 FROM app.payment_ledger_entries l
		         WHERE l.app_id = a.id AND l.entry_type = 'refund_debit'
		           AND l.payment_order_id = o.id))
		FROM app.payment_apps a
		LEFT JOIN app.payment_orders o ON %s
		WHERE %s
		GROUP BY 1, 2, 3 ORDER BY a.name`, successRateSQL, on, where), nil, b.args...)
	if err != nil {
		return nil, fmt.Errorf("merchant apps table: %w", err)
	}
	defer rows.Close()
	out := []MerchantAppRow{}
	byApp := map[string]*MerchantAppRow{}
	for rows.Next() {
		var row MerchantAppRow
		var paid, refunded int64
		if err := rows.Scan(&row.AppID, &row.Name, &row.Status, &row.TxCount, &row.SuccessRate, &paid, &refunded); err != nil {
			return nil, fmt.Errorf("scan merchant apps table: %w", err)
		}
		if paid > 0 {
			rate := float64(refunded) / float64(paid)
			row.RefundRate = &rate
		}
		row.TPVByCurrency = MoneyByCurrency{}
		row.RevenueByCurrency = MoneyByCurrency{}
		out = append(out, row)
		byApp[row.AppID] = &out[len(out)-1]
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant apps table: %w", err)
	}
	// Per-app paid volume + net revenue per currency (own builders keep
	// numbering trivial).
	volConds, volArgs := merchantScope(p, env, "o.created_at", "o", "a", "o.app_id")
	volRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.app_id::text, o.currency, COALESCE(SUM(o.amount) FILTER (WHERE o.status = 'paid'), 0)::text
		FROM app.payment_orders o
		JOIN app.payment_apps a ON a.id = o.app_id
		WHERE %s GROUP BY 1, 2`, volConds), nil, volArgs...)
	if err != nil {
		return nil, fmt.Errorf("merchant apps volume: %w", err)
	}
	for volRows.Next() {
		var appID, currency, gross string
		if err := volRows.Scan(&appID, &currency, &gross); err != nil {
			volRows.Close()
			return nil, fmt.Errorf("scan merchant apps volume: %w", err)
		}
		if row, ok := byApp[appID]; ok {
			row.TPVByCurrency[currency] = gross
		}
	}
	volRows.Close()
	if err := volRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant apps volume: %w", err)
	}
	revConds, revArgs := merchantScope(p, env, "l.created_at", "o", "a", "l.app_id")
	revRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT l.app_id::text, l.currency,
		       (COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)
		        - COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0))::text
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s GROUP BY 1, 2`, revConds), nil, revArgs...)
	if err != nil {
		return nil, fmt.Errorf("merchant apps revenue: %w", err)
	}
	for revRows.Next() {
		var appID, currency, net string
		if err := revRows.Scan(&appID, &currency, &net); err != nil {
			revRows.Close()
			return nil, fmt.Errorf("scan merchant apps revenue: %w", err)
		}
		if row, ok := byApp[appID]; ok {
			row.RevenueByCurrency[currency] = net
		}
	}
	revRows.Close()
	if err := revRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate merchant apps revenue: %w", err)
	}
	return out, nil
}

// merchantScope builds "range + environment + optional provider + org +
// app" with strictly ordered placeholders ($1 from, $2 to, $3 env,
// [$4 provider,] [$5 org,] [$6 app]) and returns the matching values.
// timeCol carries the range (orders or ledger timestamps); envCol carries
// environment+provider (always the orders alias); appsAlias owns org_id;
// appCol is the fully qualified app-id column.
func merchantScope(p Params, env, timeCol, envCol, appsAlias, appCol string) (string, []any) {
	conds := []string{
		timeCol + " >= $1",
		timeCol + " < $2",
		envCol + ".environment = $3",
	}
	args := []any{p.From, p.To, env}
	n := 4
	if p.Provider != "" {
		conds = append(conds, fmt.Sprintf("%s.provider = $%d", envCol, n))
		args = append(args, p.Provider)
		n++
	}
	if len(p.OrgIDs) == 1 {
		conds = append(conds, fmt.Sprintf("%s.org_id = $%d::uuid", appsAlias, n))
		args = append(args, p.OrgIDs[0])
		n++
	} else if len(p.OrgIDs) > 1 {
		conds = append(conds, fmt.Sprintf("%s.org_id = ANY($%d::uuid[])", appsAlias, n))
		args = append(args, p.OrgIDs)
		n++
	}
	if p.AppID != "" {
		conds = append(conds, fmt.Sprintf("%s = $%d::uuid", appCol, n))
		args = append(args, p.AppID)
	}
	return strings.Join(conds, " AND "), args
}

// MerchantSettlements builds a ledger-based statement: per-currency
// blocks with gross/fees/refunds/net, opening/closing balances,
// per-provider splits, and itemized entries (capped). Totals always equal
// the ledger sum for the period (asserted in tests).
func (r *PostgresRepository) MerchantSettlements(ctx context.Context, p Params, env, currency string, entryLimit int) (Settlement, error) {
	out := Settlement{Timezone: Timezone, Blocks: []SettlementBlock{}}
	if len(p.OrgIDs) == 0 {
		return out, fmt.Errorf("settlements require an org scope")
	}
	orgID := p.OrgIDs[0]
	if entryLimit <= 0 || entryLimit > 5000 {
		entryLimit = 500
	}
	var orgName, businessName string
	if err := r.db.QueryRowEx(ctx, `SELECT name, COALESCE(business_name, '') FROM app.organizations WHERE id = $1::uuid`,
		nil, orgID).Scan(&orgName, &businessName); err != nil {
		return out, fmt.Errorf("settlement org: %w", err)
	}
	out.OrgID, out.OrgName, out.BusinessName = orgID, orgName, businessName
	out.From, out.To = p.From, p.To
	out.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	out.StatementID = fmt.Sprintf("STMT-%s-%s-%s-%s", orgID[:8], p.From.Format("20060102"), p.To.Format("20060102"), currency)

	currencies := []string{currency}
	if currency == "" {
		crows, err := r.db.QueryEx(ctx, `
			SELECT DISTINCT l.currency FROM app.payment_ledger_entries l
			JOIN app.payment_apps a ON a.id = l.app_id
			WHERE a.org_id = $1::uuid ORDER BY 1`, nil, orgID)
		if err != nil {
			return out, fmt.Errorf("settlement currencies: %w", err)
		}
		currencies = nil
		for crows.Next() {
			var c string
			if err := crows.Scan(&c); err != nil {
				crows.Close()
				return out, fmt.Errorf("scan settlement currencies: %w", err)
			}
			currencies = append(currencies, c)
		}
		crows.Close()
		if err := crows.Err(); err != nil {
			return out, fmt.Errorf("iterate settlement currencies: %w", err)
		}
	}
	for _, cur := range currencies {
		block, truncated, err := r.settlementBlock(ctx, p, env, orgID, cur, entryLimit)
		if err != nil {
			return out, err
		}
		out.Blocks = append(out.Blocks, block)
		out.Truncated = out.Truncated || truncated
	}
	return out, nil
}

func (r *PostgresRepository) settlementBlock(ctx context.Context, p Params, env, orgID, currency string, entryLimit int) (SettlementBlock, bool, error) {
	block := SettlementBlock{Currency: currency, ByProvider: []SettlementProvider{}, Entries: []SettlementEntry{}}
	moneyConds, moneyArgs := merchantScope(p, env, "l.created_at", "o", "a", "l.app_id")
	var gross, fees, refunds, reversals string
	if err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
		SELECT COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'payment_credit'), 0)::text,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)::text,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_debit'), 0)::text,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'refund_fee_reversal_credit'), 0)::text
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s AND l.currency = $%d`, moneyConds, len(moneyArgs)+1), nil,
		append(moneyArgs, currency)...).Scan(&gross, &fees, &refunds, &reversals); err != nil {
		return block, false, fmt.Errorf("settlement money: %w", err)
	}
	block.Gross, block.Fees, block.Refunds, block.FeeReversals = gross, fees, refunds, reversals
	block.NetSettled = moneyExpr(gross, fees, refunds, reversals)
	balConds, balArgs := "a.org_id = $1::uuid", []any{orgID}
	if p.AppID != "" {
		balConds += " AND a.id = $2::uuid"
		balArgs = append(balArgs, p.AppID)
	}
	balanceAt := func(at time.Time) (string, error) {
		var bal string
		err := r.db.QueryRowEx(ctx, fmt.Sprintf(`
			SELECT (COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'credit'), 0)
			        - COALESCE(SUM(l.amount) FILTER (WHERE l.direction = 'debit'), 0))::text
			FROM app.payment_ledger_entries l
			JOIN app.payment_apps a ON a.id = l.app_id
			WHERE %s AND l.currency = $%d AND l.created_at < $%d`, balConds, len(balArgs)+1, len(balArgs)+2),
			nil, append(append([]any{}, balArgs...), currency, at)...).Scan(&bal)
		return bal, err
	}
	var err error
	if block.OpeningBalance, err = balanceAt(p.From); err != nil {
		return block, false, fmt.Errorf("settlement opening: %w", err)
	}
	if block.ClosingBalance, err = balanceAt(p.To); err != nil {
		return block, false, fmt.Errorf("settlement closing: %w", err)
	}
	provRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT o.provider,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'payment_credit'), 0)::text,
		       COALESCE(SUM(l.amount) FILTER (WHERE l.entry_type = 'platform_fee_debit'), 0)::text,
		       COUNT(DISTINCT o.id)
		FROM app.payment_ledger_entries l
		JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s AND l.currency = $%d AND l.entry_type IN ('payment_credit', 'platform_fee_debit')
		GROUP BY 1 ORDER BY 2 DESC`, moneyConds, len(moneyArgs)+1), nil,
		append(moneyArgs, currency)...)
	if err != nil {
		return block, false, fmt.Errorf("settlement providers: %w", err)
	}
	for provRows.Next() {
		var sp SettlementProvider
		if err := provRows.Scan(&sp.Provider, &sp.Gross, &sp.Fees, &sp.Count); err != nil {
			provRows.Close()
			return block, false, fmt.Errorf("scan settlement providers: %w", err)
		}
		block.ByProvider = append(block.ByProvider, sp)
	}
	provRows.Close()
	if err := provRows.Err(); err != nil {
		return block, false, fmt.Errorf("iterate settlement providers: %w", err)
	}
	entryConds, entryArgs := merchantScope(p, env, "l.created_at", "o", "a", "l.app_id")
	// Withdrawal/adjustment entries link no order: include them (they are
	// real money movement) instead of dropping them via the env join.
	// $3 is always the environment placeholder (see merchantScope).
	entryConds = strings.Replace(entryConds, "o.environment = $3", "(o.id IS NULL OR o.environment = $3)", 1)
	entryRows, err := r.db.QueryEx(ctx, fmt.Sprintf(`
		SELECT l.id::text, l.entry_type, l.direction, l.amount::text, l.currency,
		       COALESCE(l.payment_order_id::text, ''), COALESCE(l.description, ''), l.created_at
		FROM app.payment_ledger_entries l
		LEFT JOIN app.payment_orders o ON o.id = l.payment_order_id
		JOIN app.payment_apps a ON a.id = l.app_id
		WHERE %s AND l.currency = $%d
		ORDER BY l.created_at DESC LIMIT %d`, entryConds, len(entryArgs)+1, entryLimit+1), nil,
		append(entryArgs, currency)...)
	if err != nil {
		return block, false, fmt.Errorf("settlement entries: %w", err)
	}
	defer entryRows.Close()
	truncated := false
	for entryRows.Next() {
		var e SettlementEntry
		var created time.Time
		if err := entryRows.Scan(&e.ID, &e.Type, &e.Direction, &e.Amount, &e.Currency, &e.OrderID, &e.Description, &created); err != nil {
			return block, false, fmt.Errorf("scan settlement entries: %w", err)
		}
		e.CreatedAt = created.UTC().Format(time.RFC3339)
		if len(block.Entries) >= entryLimit {
			truncated = true
			continue
		}
		block.Entries = append(block.Entries, e)
	}
	if err := entryRows.Err(); err != nil {
		return block, false, fmt.Errorf("iterate settlement entries: %w", err)
	}
	return block, truncated, nil
}

// moneyExpr computes gross - fees - refunds + fee_reversals on numeric
// strings (ledger scale, 2dp).
func moneyExpr(gross, fees, refunds, reversals string) string {
	var g, f, r, v float64
	fmt.Sscanf(gross, "%f", &g)
	fmt.Sscanf(fees, "%f", &f)
	fmt.Sscanf(refunds, "%f", &r)
	fmt.Sscanf(reversals, "%f", &v)
	return fmt.Sprintf("%.2f", g-f-r+v)
}
