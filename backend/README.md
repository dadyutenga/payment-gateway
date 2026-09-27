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
# fill in DATABASE_URL, AUTH_JWT_SECRET, APP_ENCRYPTION_KEY at minimum

go run ./cmd/migrate -action up
go run ./cmd/seed-admin -email admin@example.com
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
go run ./cmd/seed-admin -email ...   # create/promote an admin (never via signup)
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
| `AUTH_ALLOW_PUBLIC_REGISTER` | `false` (default): only the first account may self-register, then signup closes |
| `PAYMENTS_LIVE_MAX_TXN_AMOUNT` | per-transaction live cap (default `5000000`) |
| `PAYMENTS_LIVE_DAILY_VOLUME_CAP` | per-app per-currency live daily cap (default `50000000`) |

## Admin access

User accounts and password hashes live in `app.users`. Self-registration
**never** grants admin — there is no `ADMIN_EMAILS` allowlist anymore. The
only automated way to mint an admin is:

```
go run ./cmd/seed-admin -email admin@example.com          # generates a password
go run ./cmd/seed-admin -email admin@example.com -password '...'
```

It creates the account if missing, promotes an existing non-admin, and is
a no-op for existing admins. Self-registration is closed by default: only
the very first account (empty users table) may self-register to bootstrap
the deployment; afterwards `POST /api/v1/auth/register` returns
`403 registration_disabled` unless `AUTH_ALLOW_PUBLIC_REGISTER=true`.
Admin rights are re-read from `app.users` on every request, so revoking
`is_admin` takes effect immediately instead of lingering in the token
until `AUTH_TOKEN_TTL` expires.

## Signup verification (OTP)

- `POST /api/v1/auth/otp/request` `{channel: email|sms, purpose, phone?}`
  and `POST /api/v1/auth/otp/verify` `{channel, purpose, code}` (both
  session-authenticated).
- Codes are 6 digits, bcrypt-hashed in `app.otp_codes`, expire in 10
  minutes, max 5 requests/hour per user, locked after 5 wrong attempts
  (re-request clears the lock).
- SMS numbers must be Tanzanian mobiles (`+2556…` / `+2557…`).
- No mail/SMS provider ships with this repo: `Mailer`/`SMSSender` are
  narrow interfaces (see `internal/platform/auth`) with log-only stubs
  wired in `httpserver.New`. Production must wire a real provider (e.g.
  Beem Africa for TZ SMS) — until then OTP requests return
  `503 otp_not_configured`.
- `GET /api/v1/admin/me` reports `email_verified`, `phone`, and
  `phone_verified` for the frontend flow.

## KYC (verification) & sandbox gating

- `POST /api/v1/orgs/{orgID}/kyc` `{business_name, tin, id_document_url}`
  (owner/`manage_org`), `GET /api/v1/orgs/{orgID}/kyc` for status;
  `POST .../kyc/document` (multipart `document`, JPEG/PNG/WEBP/PDF, ≤5MB,
  stored under `backend/uploads/kyc/`) and `GET .../kyc/document` to fetch.
- Submission flips `organizations.kyc_status` to `submitted` (review
  approve/reject is a later admin block; `000039_kyc_submissions` keeps
  the evidence + review queue).
- **Gates:** live API keys (merchant create/rotate) and live orders
  return `403 kyc_required` until the org is `verified`. Sandbox keys and
  sandbox orders skip the gate. The order's environment comes from the
  API key — never from the request body.
- **Live caps:** `PAYMENTS_LIVE_MAX_TXN_AMOUNT` (per txn) and
  `PAYMENTS_LIVE_DAILY_VOLUME_CAP` (ledger-summed per app/currency/UTC
  day) return `422 live_txn_cap_exceeded` / `422 live_daily_cap_exceeded`.
  Sandbox orders skip both.
- Frontend: `/signup` (email + OTP verify), `/onboarding/kyc/{orgID}`
  (business name, TIN, document upload), and a sandbox-mode banner while
  the active org is unverified.

## Organizations & roles

Apps belong to organizations (`payment_apps.org_id`, NOT NULL).
Membership lives in `app.org_members` (replacing `payment_app_members`,
which is left untouched as a legacy record). Roles and their powers live
in exactly one place — `internal/modules/orgs/model.go` (`Can`):

| role | reads | withdrawals | webhooks/keys/apps | members | org settings/delete |
|---|---|---|---|---|---|
| owner | ✓ | ✓ | ✓ | ✓ | ✓ |
| finance | ✓ | ✓ | — | — | — |
| developer | ✓ | — | ✓ | — | — |
| viewer | ✓ | — | — | — | — |

Every merchant route resolves the caller's active role server-side from
the session and never trusts a client-supplied app/org id. Permission
failures return `403 forbidden` JSON (never raw errors). Revoking the
last owner is refused (`409 last_owner`).

Org endpoints (all session-authenticated): `POST/GET /api/v1/orgs`,
`GET/PATCH/DELETE /api/v1/orgs/{orgID}`, `GET .../members`,
`POST .../invites` (owner, existing accounts only),
`POST .../accept`, `PATCH/DELETE .../members/{userID}` (owner),
`POST .../leave`. Deleting an org with apps is refused (`409`).
Finance/owner members additionally get
`POST /api/v1/merchant/apps/{id}/withdrawals/{withdrawalID}/approve|reject`.

## Merchant apps & API keys (self-service)

Merchants manage their own apps — no admin ticket needed:

- `POST /api/v1/merchant/apps` `{org_id, name, description?}` creates an
  app inside one of the caller's orgs. Requires the develop permission
  (owner/developer); finance/viewer get 403, non-members get 403.
- The app's first key follows the org's KYC status: verified orgs get a
  **live** key, everyone else a **sandbox** key — the client never
  chooses, and the same rule applies to admin-created apps, so no path
  mints a live key for an unverified org by accident.
- `PATCH /api/v1/merchant/apps/{id}` renames an app (owner/developer).
- Keys carry an optional display-only **label** (≤60 chars, never secret):
  set at creation (`POST .../api-keys` `{environment, label?}`) or
  renamed later (`PATCH .../api-keys/{keyID}` `{label}`). Listed
  alongside prefix/env/status everywhere keys appear.

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
