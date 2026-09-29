# LipaGO — frontend

React panel for LipaGO: signup, organizations, apps, API keys, providers,
withdrawals, orders/ledger/events. It talks only to the Go backend via
`VITE_API_BASE_URL`.

## Setup

```
cp .env.example .env
# VITE_API_BASE_URL — where you deployed backend/ (defaults to http://localhost:8080 in dev)

npm install
npm run dev
```

```
npm run build      # production build → dist/
npm run preview    # preview the production build locally
```

## Signing in

1. Create the first admin on the backend with
   `go run ./cmd/seed-admin -email you@example.com` (self-registration
   never grants admin).
2. Sign in at `/admin/login` — operators land in the admin panel:
   overview dashboard, platform analytics, KYC review, apps oversight,
   providers, withdrawals/payout recording.
3. Merchants sign up at `/register` (or `/signup`), verify email, create
   an organization at `/onboarding/create-org`, and work in sandbox mode
   immediately. Live API keys/payments unlock after verification is
   submitted at `/onboarding/kyc/:orgId` and approved. Merchant sign-in
   is at `/login`.

## Pages

| Route | Purpose |
|---|---|
| `/` | public welcome with merchant/operator doors |
| `/register`, `/login` | merchant signup + sign-in (`/signup`, `/signin` kept as aliases) |
| `/admin/login` | operator sign-in (separate path + token audience) |
| `/admin` | operator dashboard (tenants, verification, payouts) |
| `/admin/analytics` | platform overview KPIs, deltas, charts |
| `/admin/analytics/providers` | per-provider volume, quality, latency, channels |
| `/admin/analytics/merchants` | top orgs, signup funnel, dormant, churn risk |
| `/admin/analytics/failures` | ranked failure reasons with samples |
| `/admin/ops` | stuck orders, unreconciled, negative balances, aging, webhooks |
| `/admin/orgs/:orgId` | operator org detail (profile, members, verification) |
| `/admin/payments` | orders/ledger/events/deliveries/metrics ops |
| `/admin/payments/apps` | cross-org apps oversight (read-only) |
| `/admin/payments/apps/:id` | single app detail — orders, ledger, withdrawals |
| `/admin/payments/withdrawals` | payout recording (dispatch, mark paid/failed) |
| `/admin/payments/providers` | configure payment provider credentials |
| `/admin/kyc` | verification review queue (approve/reject, per-org live limits) |
| `/merchant` | merchant dashboard (balance, apps, payouts, recent payments) |
| `/merchant/apps` | merchant app list (org-scoped) + self-service app creation |
| `/merchant/apps/:id` | app detail — payments, webhooks, API keys, withdrawals, deliveries |
| `/merchant/payments|withdrawals|webhooks|api-keys|deliveries` | cross-app aggregate views |
| `/merchant/analytics`, `/merchant/settlements` | resolve the org, then land on org analytics/settlements |
| `/org/:orgId/analytics` | KPI overview, per-app comparison |
| `/org/:orgId/analytics/methods|peak-hours|customers|failures` | mix, heatmap, repeat payers, reasons |
| `/org/:orgId/settlements` | ledger statements (CSV/PDF export for owner/finance) |
| `/onboarding/create-org` | create an organization (one per account) |
| `/onboarding/kyc/:orgId` | verification submission (business name, TIN, ID doc) |
| `/org/:orgId/members` | member roles & invites |
| `/org/:orgId/settings` | tabbed settings (general, verification, limits, security, notifications, branding, payouts, danger) |

Unverified orgs show a sandbox-mode banner (live keys/payments blocked).

## Notes

- Only the shadcn/ui primitives actually used by these pages are
  included (`badge`, `button`, `card`, `dialog`, `dropdown-menu`,
  `input`, `skeleton`, `sonner`, `table`, `tabs`). Add more via the
  shadcn CLI (`components.json` is already configured) if you extend the
  UI.
- No SEO/marketing tooling — this is an internal admin tool, not a
  public site.
