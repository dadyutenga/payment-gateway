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
