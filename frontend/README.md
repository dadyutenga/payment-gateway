# Payments Gateway — admin frontend

React admin panel for the Payments Gateway: apps, API keys, providers,
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

1. Add your email to `ADMIN_EMAILS` in the backend's `.env`.
2. Create the account from `/signin` using a password of at least 12 characters.
3. Sign in at `/signin` — you land in the admin panel with full access:
   create payment apps, generate API keys, configure providers, review
   orders/ledger, approve or reject withdrawals.

## Pages

| Route | Purpose |
|---|---|
| `/admin/payments` | overview / metrics |
| `/admin/payments/apps` | create apps, generate/revoke API keys, manage members |
| `/admin/payments/apps/:id` | single app detail — orders, ledger, webhook endpoints |
| `/admin/payments/withdrawals` | review and act on withdrawal requests |
| `/admin/payments/providers` | configure payment provider credentials |

## Notes

- Only the shadcn/ui primitives actually used by these pages are
  included (`badge`, `button`, `card`, `dialog`, `dropdown-menu`,
  `input`, `skeleton`, `sonner`, `table`, `tabs`). Add more via the
  shadcn CLI (`components.json` is already configured) if you extend the
  UI.
- No SEO/marketing tooling — this is an internal admin tool, not a
  public site.
