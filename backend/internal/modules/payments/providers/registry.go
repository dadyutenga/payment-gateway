// Package providers holds the fixed set of implemented payment provider
// adapter kinds, keyed by the "provider" string stored on each admin-managed
// app.payment_provider_accounts row.
//
// Unlike SMS's provider.Provider (stateless — credentials passed per-Send
// call), provider.PaymentProvider's methods are bound to a struct built at
// construction time, so this registry holds constructors rather than
// ready-made instances. Adding a new provider kind is one adapter package
// plus one line here — no schema or admin-system change needed.
package providers

import (
	"azsubay-payments-gateway/internal/modules/payments/provider"
	"azsubay-payments-gateway/internal/modules/payments/providers/sandbox"
	"azsubay-payments-gateway/internal/modules/payments/providers/sonicpesa"
)

var Registry = map[string]provider.Constructor{
	"sonicpesa": func(baseURL string, credentials map[string]string) provider.PaymentProvider {
		return sonicpesa.New(sonicpesa.Config{
			BaseURL:   baseURL,
			APIKey:    credentials["api_key"],
			APISecret: credentials["api_secret"],
		})
	},
	// sandbox is the deterministic simulator for merchant integration
	// testing (no network, no real money). Wired to an admin-managed
	// provider account like any real kind; settle_seconds and
	// always_fail come from that account's credentials. Live orders
	// can never use it — payments.Service rejects the combination.
	"sandbox": func(_ string, credentials map[string]string) provider.PaymentProvider {
		return sandbox.New(sandbox.ConfigFromCredentials(credentials))
	},
}
