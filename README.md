# AZSUBAY Payments Gateway (standalone)

A self-contained copy of AZSUBAY's Payments Gateway — the module that lets
an app accept payments through multiple providers (SonicPesa today, more
addable), tracks a real ledger balance per app, and handles withdrawals —
extracted from the main AZSUBAY monorepo so it can be deployed and run on
its own.

This is **not connected to AZSUBAY's production database or accounts in
any way**. It's your own copy: your own database, your own Supabase auth
project, your own admin list, your own provider credentials. Nothing here
talks to azsubay.com.

## What's inside

```
backend/    Go API + background worker + migrations
frontend/   React admin panel — Apps, Withdrawals, Providers, Orders/Ledger/Events
```

## Quick start

1. **Database**: point the gateway at any Postgres 14+ database. The
   migrations create their own `app` schema, including local user accounts.

2. **Backend**:
   ```
   cd backend
   cp .env.example .env   # fill in DATABASE_URL, AUTH_JWT_SECRET, APP_ENCRYPTION_KEY, ADMIN_EMAILS
   go run ./cmd/migrate -action up
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

4. Put your email in `ADMIN_EMAILS`, start the frontend, then create that
   account from its sign-in page. That account can now sign in
   to the admin panel, register apps, generate API keys, configure
   payment providers, and manage withdrawals.

## Handing this to someone else

Everything above is env-var driven — there's no AZSUBAY-specific
configuration baked into the code. Zip this folder (or push it to its own
git repo) and hand it over; whoever receives it fills in their own `.env`
files and it's a fully working, independent Payments Gateway. See each
package's README for the full environment variable reference.
