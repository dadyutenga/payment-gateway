# Payments Gateway — backend

Go API + background jobs + migrations for the Payments Gateway. Talks to
its own Postgres database and local Go-managed authentication — nothing
here depends on AZSUBAY's production systems.

## Requirements

- Go 1.25+
- A Postgres 14+ database

## Setup

```
cp .env.example .env
# fill in DATABASE_URL, AUTH_JWT_SECRET, APP_ENCRYPTION_KEY, ADMIN_EMAILS at minimum

go run ./cmd/migrate -action up
go run ./cmd/api
```

`cmd/api` is a single process: it serves the HTTP API **and** runs the
background jobs (webhook delivery retries, payment reconciliation, and —
if enabled — automated payouts) on a 30s ticker. For most deployments
that's all you need.

## Optional: separate worker process

`cmd/worker` runs the same background jobs standalone. It's safe to run
alongside `cmd/api` — webhook deliveries use `FOR UPDATE SKIP LOCKED`
claim-based locking so they never double-process the same job, while payment
and payout reconciliation are safe via per-order `SELECT ... FOR UPDATE`
row locking in `ApplyWebhookEvent` plus partial unique indexes
(`000029_ledger_idempotency`) that allow at most one `payment_credit` /
`refund_debit` per order — useful if you want
to scale the API and the background work independently, or deploy them
on different schedules/instances.

```
go run ./cmd/worker
```

## Commands

```
go run ./cmd/migrate -action up      # apply migrations
go run ./cmd/migrate -action down    # roll back one migration
go build ./...                        # build all binaries
go vet ./...
go test ./...
```

## Environment variables

See `.env.example` for the full reference with inline comments. The
required ones to get running at all:

| Variable | What it's for |
|---|---|
| `DATABASE_URL` | Postgres connection string |
| `AUTH_JWT_SECRET` | signs and verifies local user sessions |
| `APP_ENCRYPTION_KEY` | encrypts provider credentials at rest (`openssl rand -base64 32`) |
| `ADMIN_EMAILS` | comma-separated bootstrap admin allowlist (replace example addresses!) |
| `AUTH_ALLOW_PUBLIC_REGISTER` | `false` (default): only the first account may self-register, then signup closes |

## Admin access

User accounts, password hashes, and roles live in `app.users`. Email
addresses in `ADMIN_EMAILS` receive the admin role on registration — put
your first administrator there before creating the account, and never
deploy with the example addresses still listed (anyone registering a
listed address would become admin).

Self-registration is closed by default: only the very first account (empty
users table) may self-register to bootstrap the deployment; afterwards
`POST /api/v1/auth/register` returns `403 registration_disabled` unless
`AUTH_ALLOW_PUBLIC_REGISTER=true`. Admin rights are re-read from
`app.users` on every request, so revoking `is_admin` takes effect
immediately instead of lingering in the token until `AUTH_TOKEN_TTL`
expires.

## Adding a payment provider

Providers implement the interface in
`internal/modules/payments/provider/types.go` and register themselves in
`internal/modules/payments/providers/registry.go`. `providers/sonicpesa`
is a complete reference implementation to copy from. Providers with a
verified refund API additionally implement `provider.Refunder`; SonicPesa
documents no refund endpoint (verified 2026-09-23), so its adapter returns
`provider.ErrRefundNotSupported` and refunds fall back to local ledger
reversal until a real contract is confirmed.

## API surface

- `POST /api/v1/payments/orders` — create a payment order (public, app-API-key auth)
- `GET /api/v1/payments/orders/{paymentID}` — order status (public, auto-refreshes stale orders)
- `POST /api/v1/payments/orders/{paymentID}/refresh` — force a live provider status check (public, app-API-key auth)
- `POST /api/v1/payments/webhooks/{provider}` — inbound provider webhooks (public)
- `POST /api/v1/admin/payments/orders/{id}/refund` — refund a paid order (admin-auth).
  Optional JSON body `{amount?, currency?, reason?}`; omitted amount refunds
  the full remaining amount. Currency must match the order; amounts above the
  remaining refundable total are rejected (422 `amount_exceeded`). Response
  `{data: {order, refund}}`. Every attempt is recorded in
  `app.payment_refunds` (order, provider refund id, amount, status, reason).
- `/api/v1/admin/payments/...` — apps, providers, orders, events,
  deliveries, withdrawals, metrics (admin-auth)
- `/api/v1/merchant/apps/...` — merchant-facing views for app members
- `/api/v1/merchant/apps/{id}/webhook-endpoints` — merchant webhook CRUD
  (GET/POST), plus `PATCH`/`DELETE .../{endpointID}` and
  `POST .../{endpointID}/test-send` (signed live probe, nothing stored)
- `/api/v1/merchant/apps/{id}/api-keys` — list (prefixes only) + create;
  `POST .../{keyID}/rotate` (old keys stay valid 24h) and
  `POST .../{keyID}/revoke` (immediate)
- `GET /api/v1/merchant/apps/{id}/deliveries` — delivery logs scoped to
  the app, plus `POST .../{deliveryID}/replay`
- All merchant routes require a signed-in session AND app membership —
  the app id always comes from the verified path, never client input.
- `GET /api/v1/health` — liveness + DB check
- `GET /api/v1/admin/me` — `{email, is_admin}` for the signed-in user

Merchant webhook event types: `payment.updated`, `payment.refunded`
(emitted after every confirmed refund, same signing as other events),
`payment.expired` (emitted when a pending order passes its TTL).
New endpoints subscribe to all three by default.

## Refunds

`POST /api/v1/admin/payments/orders/{id}/refund` refunds a paid order —
full remaining amount when `amount` is omitted, otherwise a partial refund
validated so the total never exceeds the payment. Currency must match.
The amount is claimed before any provider call, so concurrent attempts
serialize; the ledger `refund_debit` is written only after the provider
confirms (or immediately for the local manual fallback — SonicPesa
documents no refund endpoint, so its adapter reports unsupported).
Every attempt is recorded in `app.payment_refunds`.

## Order expiry

Pending orders carry `expires_at` (creation + `PAYMENTS_ORDER_TTL`,
default 30m). The expiry worker (`PAYMENTS_EXPIRY_INTERVAL`, default 1m,
runs in both `cmd/api` and `cmd/worker`) transitions overdue rows to
`expired` — no ledger movement — and emits `payment.expired`. Expired
orders are terminal: reconciliation and auto-refresh skip them, and late
provider webhooks are held for manual review instead of crediting.

## Idempotency-Key

`POST /api/v1/payments/orders` and both `POST .../withdrawals` endpoints
accept an `Idempotency-Key` header (max 128 chars, scoped per app +
endpoint). Same key + same body replays the stored response (with an
`Idempotent-Replayed: true` header); same key + different body is `409
idempotency_conflict`; transient (5xx) failures discard the claim so
retries re-execute. Records expire after 24h via the expiry sweep.

## Balances

`GET .../balance` returns per-currency breakdowns in `balances[]`; the
top-level fields describe the latest-activity currency for backward
compatibility. Amounts are never summed across currencies.

Full route list (methods, middleware) is in
`internal/platform/httpserver/app.go`.
