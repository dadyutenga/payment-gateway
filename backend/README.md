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
alongside `cmd/api` (both use `FOR UPDATE SKIP LOCKED` claim-based
locking, so they never double-process the same job) — useful if you want
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
| `ADMIN_EMAILS` | comma-separated allowlist for the admin panel |

## Admin access

User accounts, password hashes, and roles live in `app.users`. Email
addresses in `ADMIN_EMAILS` receive the admin role on registration. Add
your first administrator there before creating the account.

## Adding a payment provider

Providers implement the interface in
`internal/modules/payments/provider/types.go` and register themselves in
`internal/modules/payments/providers/registry.go`. `providers/sonicpesa`
is a complete reference implementation to copy from.

## API surface

- `POST /api/v1/orders` — create a payment order (public, app-API-key auth)
- `GET /api/v1/orders/{id}` — order status (public)
- `POST /api/v1/webhooks/{provider}` — inbound provider webhooks (public)
- `/api/v1/admin/payments/...` — apps, providers, orders, events,
  deliveries, withdrawals, metrics (admin-auth)
- `/api/v1/merchant/apps/...` — merchant-facing views for app members
- `GET /api/v1/health` — liveness + DB check
- `GET /api/v1/admin/me` — `{email, is_admin}` for the signed-in user

Full route list (methods, middleware) is in
`internal/platform/httpserver/app.go`.
