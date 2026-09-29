# LipaGO Analytics — metric definitions

Single source of truth for every number on the admin and merchant
dashboards. All analytics code (SQL, rollups, frontend) MUST match these
definitions. Timezone is **Africa/Dar_es_Salaam (EAT)** everywhere a day
or hour bucket appears; API responses state the timezone.

## Base filters

- **Environment: live only by default.** Merchants get an explicit
  sandbox toggle; sandbox rows NEVER mix into platform-wide admin
  numbers or into live merchant numbers.
- **Currency is never summed across currencies.** Every money metric is
  reported per currency.

## Definitions

- **TPV**: sum of `amount` over orders with `status = 'paid'` (gross),
  per currency.
- **Revenue**: sum of `platform_fee_debit` ledger entries minus
  `refund_fee_reversal_credit` entries (fees are NOT kept on refunded
  money), per currency, over the period.
- **Success rate**: `paid / (paid + failed + cancelled + reversed +
  expired)`. `pending`/`processing` are shown separately and EXCLUDED
  from the denominator.
- **Abandonment rate**: `expired / created`.
- **Time to pay**: `first paid transition (status_history) − created_at`
  over paid orders. Report **median (p50) and p90**, never a plain mean.
- **Provider latency**: round-trip ms of provider API calls
  (`provider_latency_ms`, measured around create/status calls);
  report **median and p95 per provider**. Time-to-confirmation is the
  time-to-pay distribution sliced per provider.
- **Repeat-customer rate**: payers with 2+ paid orders in the period /
  payers with 1+ paid orders, keyed by `payer_hash`
  (HMAC-SHA256 of the normalized phone, keyed by `ANALYTICS_PAYER_SECRET`;
  raw numbers never enter analytics). Normalization: digits only,
  Tanzanian `0…` rewritten to `255…`.
- **Dormant**: verified org with zero paid live orders in the last N days
  (default 30, configurable).
- **Churn risk**: active merchant whose trailing-14-day live volume
  dropped more than X% (default 50, configurable) versus the prior
  14 days, OR no transactions for N days after a previously steady
  pattern. Every flag shows WHY (the two windows and the drop %).
- **Signup funnel**: registered → email verified → org created → KYC
  submitted → KYC verified → first live transaction, with conversion
  and median time between steps.

## Failure codes (`failure_code` enum)

`expired` (TTL, no money moved) · `cancelled` · `provider_declined`
(provider said no; `failure_message` carries their status text) ·
`timeout` (provider call deadline) · `system_error` (anything else).
Stamped at final transitions only; expiry stamps in SQL, the rest in
`applyProviderStatusUpdate`/webhook/create paths. Best-effort: analytics
stamps never fail money writes.

## Channels (`channel` enum)

`M-PESA | AIRTEL | TIGO | HALOPESA | OTHER`, extracted from provider
responses/webhooks (`channel/network/...` keys). Unrecognized non-empty
input → `OTHER`; empty → NULL (untouched).

## Rollups (cache, rebuildable)

- `analytics_daily_app(day, org, app, env, provider, channel, currency)`:
  created/succeeded/failed(expired split out)/expired, gross, p50/p90 TTP.
- `analytics_daily_app_money(day, org, app, currency)`: fees, refunds,
  refund_total — separate grain so fees can never double-count across
  provider/channel rows.
- `analytics_hourly_app(hour, org, app, env)`: counts only.
- Refresh: whole EAT days, delete+insert in one transaction (idempotent).
  Worker refreshes yesterday+today every reconciliation tick; use rollups
  for ranges beyond today, live queries for today + ops views.
- **Statements/exports money totals come from the LEDGER, never rollups.**
- Rebuild: `go run ./cmd/backfill-analytics -from YYYY-MM-DD -to
  YYYY-MM-DD [-payer-hash-backfill]`. Back up `payment_orders` first
  (`pg_dump -t app.payment_orders`); refresh itself only writes the
  `analytics_*` tables.

## Privacy

- Raw payer phones live only in `payment_orders.buyer_phone`.
- Analytics, rollups, exports, and masked displays use `payer_hash` or
  server-side masking (`+255 7** *** 123`); full numbers never appear.
- Merchant endpoints are org-scoped server-side; developers see
  operational metrics but no payer-level detail.

## Config

`ANALYTICS_PAYER_SECRET` (falls back to the delivery signing secret in
dev; set distinctly in production) · thresholds live with each endpoint
(Block 2/4) and are stated in responses.

## Endpoint reference

All responses are `{data: ...}` with `timezone: "Africa/Dar_es_Salaam"`.
Common params: `from`/`to` (YYYY-MM-DD, EAT day bounds, default trailing
30d, max 366d), `granularity` (hour/day/week/month; hour capped to 7d),
`provider`, `currency`, `org_id` (admin filter), `app_id` (must belong to
the org), `page`/`per_page` (≤200). Bad input → `400 invalid_request`.

### Admin (`/api/v1/admin/analytics/...`, aud=admin, 60/min per user)

| Endpoint | Returns |
|---|---|
| `GET /overview` | TPV/revenue/tx/success/abandonment/median-p90 TTP/active merchants/signups + prev-period deltas + series |
| `GET /providers` | Per-provider volume/success/failures/latency p50-p95/1h-24h signals/revenue/channels |
| `GET /merchants/top?sort=` | Orgs by TPV/revenue with trend, success + refund rates, pagination |
| `GET /merchants/signups` | Funnel series + totals + median step hours |
| `GET /merchants/dormant?dormant_days=` | Verified orgs silent N days + reason + contact |
| `GET /merchants/churn-risk?churn_drop_pct&churn_window_days=` | Drop-flagged orgs + reason + contact |
| `GET /failures` | Ranked (code, provider) + affected orgs + sample order ids |
| `GET /withdrawals` | Pending/approval queue/aging buckets/failed/avg payout hours |
| `GET /webhooks` | Success/retry/p95 latency/stuck/offenders |
| `GET /ops/stuck-orders?stuck_minutes=` | Pending/processing older than N min (cap 200) |
| `GET /ops/unreconciled` | Paid-without-ledger + ledger-without-paid (cap 200) |
| `GET /ops/negative-balances` | Sub-zero app balances |
| `GET /export/:report?format=csv` | CSV for top-merchants/signups/dormant/churn-risk/failures/withdrawals/webhook-offenders/stuck-orders/unreconciled/negative-balances — **audited** |

### Merchant (`/api/v1/orgs/:orgId/...`, aud=customer, org resolved
server-side, 60/min per user)

| Endpoint | Roles | Returns |
|---|---|---|
| `GET /analytics/overview` | all members | Revenue/gross/tx/success/abandon/TTP/avg-order + deltas + series |
| `GET /analytics/methods` | all members | Channel mix |
| `GET /analytics/peak-hours` | all members | EAT weekday×hour grid + best/worst labels |
| `GET /analytics/customers` | all, developers masked | Repeat rate, new-vs-returning, masked top payers |
| `GET /analytics/failures` | all members | Org failure breakdown |
| `GET /analytics/apps` | all members | Per-app comparison |
| `GET /settlements?format=json` | all members | Ledger statement blocks |
| `GET /settlements?format=csv\|pdf` | owner/finance, 10/min, audited | File download (PDF needs `?currency=`) |

`?environment=live|sandbox` (live default) on every merchant endpoint.
Viewers cannot export (403); developers get aggregates with
`payer_detail_hidden: true`.

## Thresholds

Dormant N days (default 30) · churn drop X% over trailing W days
(defaults 50/14, prior window ≥5 txns to cut noise) · stuck orders N min
(default 30) · webhook stuck N min (default 30) · withdrawal aging
buckets <1h/1-6h/6-24h/1-3d/>3d. All overridable per request and echoed
in responses; churn/dormant rows always state WHY.

## Privacy rules

- Raw payer phones live only in `payment_orders.buyer_phone` (required
  for provider calls + search). Analytics keys everything by
  `payer_hash` (HMAC-SHA256, `ANALYTICS_PAYER_SECRET`, TZ-normalized).
- Masked display (`+255 7** *** 123`) is derived server-side at query
  time; full numbers never appear in analytics JSON, rollups, CSVs, or
  PDFs. Verified by grep: only `repository_merchant.go` reads
  `buyer_phone`, straight into `mask()`.
- No platform-wide number is ever exposed to customers (org scope
  enforced in SQL + integration-tested per endpoint).
- Exports carry merchant data → audited (`audit_log`,
  `analytics.export` / `settlement-csv|pdf`).

## Rollup design and rebuild

Grain: `analytics_daily_app(day, org, app, env, provider, channel,
currency)` counts+gross+TTP percentiles; `analytics_daily_app_money`
fees/refunds per app+currency (separate grain — fees can never
double-count); `analytics_hourly_app` counts. Worker refreshes
yesterday+today every reconciliation tick (delete+insert per EAT day =
idempotent). Dashboards read rollups for whole past days, live tables
for edges/today, with live fallback for missing days (fresh deploys) —
proven equal by `TestOverviewRollupLiveEquivalence`.
Rebuild any range: `go run ./cmd/backfill-analytics -from YYYY-MM-DD
-to YYYY-MM-DD [-payer-hash-backfill]`. Back up `app.payment_orders`
first (`pg_dump -t app.payment_orders`); refresh writes only
`analytics_*` tables.

## Performance (Block 6 proof, 2026-09-29, dev laptop, 1M orders)

- Overview 30d: ~650–1000ms cold, ~0ms warm (60s TTL service cache).
- Top merchants: ~0.7–1.8s cold, ~0ms warm. Simple queries 200–500ms.
- Design: whole EAT days read pre-aggregated rollups; partial edges +
  today read live; TTP percentiles read `first_paid_at` (no history
  join); independent queries run concurrently; per-user rate limit
  (60/min) + range caps (366d, hour ≤ 7d) guard the DB.
- Honest note: the 500ms cold target is not fully met on 1M rows on dev
  iron (noisy box, ±2x run variance); warm-cache p95 is ~0ms and every
  simple endpoint is in budget. Next levers if cold must drop further:
  approximate TTP from rollup percentiles, or a read replica.
- Seed: `go run ./cmd/seed-analytics -orgs 200 -orders 1000000
  -payer-secret ...` (idempotent, `analytics-seed-` prefix, NEVER on
  prod). Bench: same command with `-seed=false`.
