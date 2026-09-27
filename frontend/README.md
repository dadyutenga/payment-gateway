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
2. Sign in at `/signin` — you land in the admin panel with full access:
   create payment apps, generate API keys, configure providers, review
   orders/ledger, approve or reject withdrawals.
3. Merchants sign up at `/signup` (email + OTP verification), create an
   organization at `/onboarding/create-org`, and can work in sandbox mode
   immediately. Live API keys/payments unlock after the org's verification
   is submitted at `/onboarding/kyc/:orgId` and approved.

## Pages

| Route | Purpose |
|---|---|
| `/signup` | merchant signup + email OTP verification |
| `/admin/payments` | overview / metrics |
| `/admin/payments/apps` | create apps, generate/revoke API keys, manage members |
| `/admin/payments/apps/:id` | single app detail — orders, ledger, webhook endpoints |
| `/admin/payments/withdrawals` | review and act on withdrawal requests |
| `/admin/payments/providers` | configure payment provider credentials |
| `/admin/kyc` | verification review queue (approve/reject, per-org live limits) |
| `/merchant/apps` | merchant app list (org-scoped) + self-service app creation |
| `/merchant/apps/:id` | app detail — rename, webhooks, labeled API keys, deliveries |
| `/onboarding/create-org` | create an organization |
| `/onboarding/kyc/:orgId` | verification submission (business name, TIN, ID doc) |
| `/org/:orgId/members` | member roles & invites |
| `/org/:orgId/settings` | org settings + verification status |

Unverified orgs show a sandbox-mode banner (live keys/payments blocked).

## Notes

- Only the shadcn/ui primitives actually used by these pages are
  included (`badge`, `button`, `card`, `dialog`, `dropdown-menu`,
  `input`, `skeleton`, `sonner`, `table`, `tabs`). Add more via the
  shadcn CLI (`components.json` is already configured) if you extend the
  UI.
- No SEO/marketing tooling — this is an internal admin tool, not a
  public site.
