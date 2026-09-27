# LipaGO payment system

LipaGO lets an app accept payments through multiple providers (SonicPesa
today, more addable), tracks a real ledger balance per app, and handles
withdrawals — with multi-tenant organizations, roles, and a merchant
self-service frontend on top.

This is **your own copy**: your own database, your own user accounts,
your own admin list, your own provider credentials. Nothing here talks
to anyone else's systems.

## What's inside

```
backend/    Go API + background worker + migrations
frontend/   React panel — signup, orgs, apps, withdrawals, providers, orders/ledger/events
```

## Quick start

1. **Database**: point the gateway at any Postgres 14+ database. The
   migrations create their own `app` schema, including local user accounts.

2. **Backend**:
   ```
   cd backend
   cp .env.example .env   # fill in DATABASE_URL, AUTH_JWT_SECRET, APP_ENCRYPTION_KEY
   go run ./cmd/migrate -action up
   go run ./cmd/seed-admin -email admin@example.com   # create the first admin
   go run ./cmd/api
   ```
   That's one process — it serves the API and runs delivery/reconciliation
   in the background. See backend/README.md for the optional separate
   worker process and everything else.

3. **Frontend**:
   ```
   cd frontend
   cp .env.example .env   # VITE_API_BASE_URL pointing at the backend
   npm install
   npm run dev
   ```

4. Sign in with the account created by `cmd/seed-admin` (self-registration
   never grants admin). Merchants can sign up from the frontend, create an
   organization, and work in sandbox mode immediately — live API keys and
   live payments unlock after their organization's verification (KYC) is
   submitted and approved.

## Handing this to someone else

Everything above is env-var driven — there's no deployment-specific
configuration baked into the code. Zip this folder (or push it to its own
git repo) and hand it over; whoever receives it fills in their own `.env`
files and it's a fully working, independent payment system. See each
package's README for the full environment variable reference.
