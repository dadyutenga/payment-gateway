# LipaGO payment system — backend

Go API + background jobs + migrations for LipaGO. Talks to
its own Postgres database and local Go-managed authentication — nothing
here depends on anyone else's production systems.

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

## Individual API naming

The current external name for the personal account track is **individual**.
New clients should use `/api/v1/individual/auth/*`,
`/api/v1/individual/apps/*`, and `/api/v1/individual/account/*`. The
session-owned account routes resolve the caller's one individual account;
the `{accountID}` form is retained for authorized deep links and still
enforces membership and account-kind checks.

The older `/api/v1/creator/*`, `/api/v1/creator/orgs/*`, and
`/api/v1/orgs/creator` routes are deprecated compatibility aliases. They
remain available for existing clients and use the same handlers, validation,
authorization, and response shapes. The public `/api/v1/c/{handle}` support
page URL is unchanged.

This is an external terminology change only. The shared internal
`organizations` table, its `account_kind` column, and stored values such as
`creator` are intentionally unchanged so merchant and individual records
continue to use the same data model.

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
| `AUTH_ALLOW_PUBLIC_REGISTER` | `true` (default): merchant self-registration open (customer accounts only, never admins) |
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
a no-op for existing admins. Self-registration is open by default
(`AUTH_ALLOW_PUBLIC_REGISTER=true`): anyone may sign up, but signup only
ever creates customer (merchant) accounts — never admins. Set
`AUTH_ALLOW_PUBLIC_REGISTER=false` to close signup entirely. Admin rights
are re-read from `app.admin_users` on every request, so deleting an admin
takes effect immediately instead of lingering in the token
until `AUTH_ADMIN_TOKEN_TTL` expires.

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
  stored in private R2 objects) and `GET .../kyc/document` to fetch through
  the authenticated backend. Creator back-side documents and selfies use the
  same private R2 path and access audit.
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
- **Creator risk posture (tighter than merchants):** creator-kind orgs
  resolve the same caps from stricter platform defaults —
  `PAYMENTS_CREATOR_LIVE_MAX_TXN_AMOUNT` (default `1000000`) and
  `PAYMENTS_CREATOR_LIVE_DAILY_VOLUME_CAP` (default `10000000`).
  Resolution order per org is admin override, then kind default
  (creator vs merchant). A creator stays below the merchant tier until
  an admin raises that org via `PATCH .../limits`; the onboarding
  survey's self-reported bands only feed the admin risk signal, never
  limits. The public support page's max amount is clamped to the same
  creator tier even for pre-verification (sandbox) orders.
- Frontend: `/signup` (email + OTP verify), `/onboarding/kyc/{orgID}`
  (business name, TIN, document upload), and a sandbox-mode banner while
  the active org is unverified.

### File storage

All new KYC and branding uploads use Cloudflare R2 through
`internal/platform/storage`. R2 uses the S3-compatible endpoint with region
`auto`, a configured custom endpoint, path-style addressing, and SDK checksum
calculation and validation only when required. Cloudflare's S3 compatibility
docs and Go SDK example were checked on 2026-10-02; the compatibility page
was last updated 2026-07-31 and the Go example 2026-04-21.

KYC objects are private under `private/kyc/` and are streamed only after the
existing organization or admin authorization check. Access is written to the
audit trail. Branding objects are separate under `public/branding/`; public
creator pages receive a five-minute presigned GET URL. No support-ticket
attachment module exists in this checkout yet, so there is no attachment call
site to migrate; it should use the same storage interface and a
`private/support/` namespace when added.

Existing local files can be copied and verified with:

```text
go run ./cmd/migrate-uploads
```

The command updates database references only after reading the uploaded R2
object back and deliberately leaves local files in place for manual cleanup.

## Creator risk posture (Part 5)

- **Expected vs actual:** `GET /api/v1/admin/analytics/merchants/volume-vs-expected[?volume_days=N]`
  (default 14) flags creator orgs whose paid live volume in their first
  N days live exceeds the survey band ceiling — surface-only, same
  posture as churn-risk (never blocks), CSV-exportable as
  `volume-vs-expected`, shown under Merchants → Volume vs expected.
- **Payout destinations:** one saved mobile-money destination per
  creator org (`GET|POST /api/v1/merchant/orgs/{orgID}/payout-destination`).
  Saves require a fresh OTP (`purpose=payout_destination`, standard OTP
  endpoints) and a verified identity whose name the attested account
  name must match (token-subset compare; mismatch is `422`). Changes
  take effect after a 24h cooling period; creator withdrawals
  (merchant and admin paths alike) must target the effective
  destination (`422 destination_mismatch` / `409 destination_cooling`),
  mobile money only. Merchant per-request destinations are unchanged.
- **Provider name lookup:** no adapter implements
  `provider.AccountNameResolver`, so saves record
  `name_match=unavailable` with the reason explicitly — surfaced in
  Settings and never skipped silently. Register the kind in
  `accountNameResolverKinds` when such an adapter lands.

## Admin: KYC review & per-org live limits

- `GET /api/v1/admin/orgs/kyc-queue[?status=submitted|all|verified|rejected|pending]`
  lists orgs holding verification files, newest first (default: the
  actionable `submitted` queue).
- `POST /api/v1/admin/orgs/{orgID}/kyc/approve` verifies (unlocks live
  keys/payments immediately);
  `POST .../kyc/reject` `{reason}` rejects (reason required, shown to
  the org). Only `submitted` files are decidable — re-deciding a closed
  file is `409 not_in_review`. The reviewer's email is recorded.
- `GET /api/v1/admin/orgs/{orgID}/kyc/document` streams the ID file for
  review (reviewers are rarely members, so no membership check).
- `PATCH /api/v1/admin/orgs/{orgID}/limits`
  `{live_max_txn_amount?, live_daily_volume_cap?}` sets per-org live
  guardrail overrides (positive decimals; empty clears back to the
  platform default). Overrides win over
  `PAYMENTS_LIVE_MAX_TXN_AMOUNT` / `PAYMENTS_LIVE_DAILY_VOLUME_CAP`
  per side; unparsable stored values fall back to the default with a
  warning, never to zero.
- Frontend: `/admin/kyc` (admin-only nav) with queue tabs, document
  viewer, approve/reject, and a limits editor. Owners see their org's
  effective limits in org settings.

## Notifications

Notifications are stored in `app.notifications` and are delivered through
the shared dispatch service. Payment, withdrawal, KYC, webhook, account,
security, creator-contribution, reconciliation, and admin-queue events use
deduplicated event keys so provider retries do not create duplicate rows.
In-app delivery is always retained for critical account events; email and
SMS are preference-controlled and processed asynchronously by the API or
`cmd/worker` background loop. The included mailer and SMS implementations
are log adapters, so production deployments must replace them with real
providers through the existing interfaces.

Customer endpoints:

- `GET /api/v1/notifications` with `unread`, `event_type`, `limit`, and
  `offset` filters.
- `POST /api/v1/notifications/{id}/read` and
  `POST /api/v1/notifications/read-all`.
- `GET|PATCH /api/v1/notifications/preferences` for personal preferences;
  merchant owners may pass `scope_kind=org&scope_id={orgID}` to manage
  workspace defaults. Individual accounts use the user scope.

Admin endpoints include the equivalent list/read routes plus
`POST /api/v1/admin/notifications/broadcast` and
`POST /api/v1/admin/orgs/{orgID}/notifications`. Broadcasts are queued and
fan out in the background to `all`, `merchant`, `creator`, `org`, or
`kyc_status` targets; `creator` is the unchanged internal account-kind value
for the externally named individual track.

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

## Webhook signing-secret rotation

Endpoint signing secrets are derived per endpoint
(`HMAC(delivery_secret, "lipago-payment-webhook:" + endpoint_id)` for
version 1). If a secret leaks, rotate it:

- `POST /api/v1/merchant/apps/{id}/webhook-endpoints/{endpointID}/rotate-secret`
  (owner/developer) bumps `secret_version` atomically and returns the new
  raw secret **exactly once**, plus the endpoint (now `secret_version` N+1).
- Versions ≥ 2 mix `:vN` into the derivation domain, so every rotation
  actually changes the key. The old secret stops verifying immediately —
  update the receiver first (or accept a short gap).
- Deliveries always sign with the version joined at claim time, so
  rotation takes effect on the next attempt; test-sends use the current
  version too. Concurrent rotations of one endpoint get `409
  rotation_conflict` (retry); unknown endpoints 404.
- The frontend webhooks tab shows `vN` per endpoint with a Rotate-secret
  button and a copy-once reveal.
- Note: the v1 derivation domain was renamed during the LipaGO rebrand
  (was `azsubay-payment-webhook:`), which changes every v1 secret.
  After upgrading past the rebrand, rotate each endpoint once so both
  sides agree on the new domain.

## Sandbox testing (simulator provider)

`sandbox` is a deterministic provider simulator for merchant integration
testing — no network calls, no real money. Wire it like any real kind:

1. Admin → Providers → create account: kind `sandbox`, any name, base URL
   anything (ignored), credentials JSON e.g.
   `{"settle_seconds": "10"}`. Set it as default for kind `sandbox`.
2. Merchant: create an app (unverified orgs get a sandbox key
   automatically), then `POST /api/v1/payments/orders` with
   `provider: "sandbox"` using that key.

Behavior:

- Orders mint `pending` instantly; the provider order id
  (`sbx_<unix>_<rand>`) embeds its creation time — that is the whole
  settlement clock, the adapter is stateless.
- `POST .../orders/{id}/refresh` (or reconciliation) reports `paid`
  once the order is older than `settle_seconds` (default 30, `0` settles
  on first check), crediting the ledger and emitting
  `payment.updated` webhooks exactly like a slow real provider.
- `{"always_fail": "true"}` in the account credentials forces `failed`
  instead (failure-path testing). Admin refunds against sandbox orders
  reverse instantly with a simulated provider refund id.
- There are no inbound webhooks for the simulator.
- **Guardrail:** `provider: "sandbox"` with a live key is rejected
  (`422` on the provider field) — simulated money can never settle
  real ledger credits, no matter which caller tries.

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
- `/api/v1/admin/analytics/...` — platform analytics (admin-auth,
  live-only): `overview`, `providers`, `merchants/top|signups|dormant|
  churn-risk`, `failures`, `withdrawals`, `webhooks`,
  `ops/stuck-orders|unreconciled|negative-balances`, plus
  `export/:report?format=csv` (audited). Params: `from`/`to`
  (YYYY-MM-DD, EAT), `granularity`, `provider`, `currency`, `org_id`.
- `/api/v1/orgs/{orgID}/analytics/...` — merchant analytics (org resolved
  server-side from the session): `overview`, `methods`, `peak-hours`,
  `customers`, `failures`, `apps`, plus `GET .../settlements?format=
  json|csv|pdf` (ledger-based statements; csv/pdf need owner/finance).
  `?environment=live|sandbox` (live default, never mixed).
- Full metric definitions, thresholds, and privacy rules:
  `docs/ANALYTICS.md`.
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
