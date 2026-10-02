package payments

import (
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"strings"

	"lipago/internal/modules/orgs"
	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"
)

// ---------- Creator support pages (Part 4, public + merchant) ----------
//
// Block 3's payment_links table has not merged, so this module carries the
// smallest link model that unblocks support payments (label + fixed/open
// amount — the shape to converge on). Money movement is NOT duplicated:
// support orders go through Service.CreateOrder, the single order path,
// with the same live-KYC gate, caps, provider resolution, and idempotency
// contract as authenticated orders.

// SupportPage serves the public support-me payload for a creator handle.
// Display-safe only: profile, category code, effective bounds, active
// buttons, usable rails. Supporter messages are never included (the
// supporters wall is future scope, default off).
func (h *Handler) SupportPage(w http.ResponseWriter, r *http.Request) {
	data, err := h.supportPageData(r)
	if err != nil {
		h.supportPublicError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Handler) supportPageData(r *http.Request) (map[string]any, error) {
	if h.orgs == nil {
		return nil, errors.New("organization access is not configured")
	}
	data, err := h.orgs.PublicCreatorSupport(r.Context(), r.PathValue("handle"))
	if err != nil {
		return nil, err
	}
	payload := buildSupportPagePayload(data)
	if strings.HasPrefix(data.Org.LogoURL, "r2://") {
		payload["logo_url"] = ""
		if h.storage != nil {
			if signed, signErr := h.storage.PresignGet(r.Context(), strings.TrimPrefix(data.Org.LogoURL, "r2://"), 300); signErr == nil {
				payload["logo_url"] = signed
			}
		}
	}
	if !data.Enabled {
		return payload, nil
	}
	app, err := h.service.ResolveSupportApp(r.Context(), data.Org.ID, data.Settings.SupportAppID)
	if err != nil {
		// Linked app deleted or moved: page degrades to profile-only
		// until the creator re-links in Settings.
		return payload, nil
	}
	min, max, err := h.service.SupportBounds(r.Context(), app, data.Settings.MinAmount, data.Settings.MaxAmount)
	if err != nil {
		return nil, err
	}
	providers, err := h.service.UsableProviderKinds(r.Context())
	if err != nil {
		return nil, err
	}
	links := make([]any, 0, len(data.Settings.Links))
	for _, l := range data.Settings.Links {
		if !l.Active {
			continue
		}
		links = append(links, map[string]any{
			"id": l.ID, "label": l.Label, "amount_mode": l.AmountMode, "amount": l.Amount,
		})
	}
	payload["enabled"] = true
	payload["min_amount"] = min
	payload["max_amount"] = max
	payload["links"] = links
	payload["providers"] = providers
	return payload, nil
}

// buildSupportPagePayload renders the public, display-safe subset of a
// creator page. Pure function so the no-leak guarantee is unit-testable:
// the key set below is exhaustive — legal names, ID numbers, documents,
// contact details, KYC state, and supporter messages can never appear
// because they are never read here.
func buildSupportPagePayload(data orgs.SupportPageData) map[string]any {
	return map[string]any{
		"display_name":  data.Org.DisplayName,
		"handle":        data.Org.Handle,
		"bio":           data.Org.Bio,
		"logo_url":      data.Org.LogoURL,
		"primary_color": data.Org.PrimaryColor,
		"category":      data.Category,
		"currency":      "TZS",
		"environment":   supportEnvironment(data.Org.KYCStatus),
		"enabled":       false,
		"min_amount":    "",
		"max_amount":    "",
		"links":         []any{},
		"providers":     []string{},
	}
}

func (h *Handler) supportPublicError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, orgs.ErrOrgNotFound):
		httputil.Error(w, http.StatusNotFound, "not_found", "Creator not found.", nil)
	case errors.Is(err, orgs.ErrSupportPageDisabled):
		httputil.Error(w, http.StatusConflict, "support_not_enabled", "This creator is not accepting support yet.", nil)
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to load the support page.", err)
	}
}

// supportEnvironment mirrors the first-key rule: verified orgs take live
// support, everything else stays sandbox.
func supportEnvironment(kycStatus string) string {
	if strings.TrimSpace(kycStatus) == "verified" {
		return "live"
	}
	return "sandbox"
}

type createSupportOrderHTTPInput struct {
	LinkID           string          `json:"link_id"`
	Amount           json.RawMessage `json:"amount"`
	Currency         string          `json:"currency"`
	Provider         string          `json:"provider"`
	BuyerName        string          `json:"buyer_name"`
	BuyerEmail       string          `json:"buyer_email"`
	BuyerPhone       string          `json:"buyer_phone"`
	SupporterMessage string          `json:"supporter_message"`
}

// CreateSupportOrder takes a public support payment through the single
// Service.CreateOrder path: same validation, live-KYC gate, caps,
// provider resolution, and Idempotency-Key contract as authenticated
// orders. The only support-specific logic is input shaping (fixed-button
// amounts, bounds, server-built metadata) — never a second ledger writer.
func (h *Handler) CreateSupportOrder(w http.ResponseWriter, r *http.Request) {
	if h.orgs == nil {
		h.log().Error("org service not wired for support route", "path", r.URL.Path)
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Support payments are not configured.", nil)
		return
	}
	data, err := h.orgs.PublicCreatorSupport(r.Context(), r.PathValue("handle"))
	if err != nil {
		h.supportPublicError(w, err)
		return
	}
	if !data.Enabled {
		httputil.Error(w, http.StatusConflict, "support_not_enabled", "This creator is not accepting support yet.", nil)
		return
	}
	app, err := h.service.ResolveSupportApp(r.Context(), data.Org.ID, data.Settings.SupportAppID)
	if err != nil {
		httputil.Error(w, http.StatusConflict, "support_not_enabled", "This creator is not accepting support yet.", nil)
		return
	}
	environment := supportEnvironment(data.Org.KYCStatus)
	if !h.requireLiveKYC(w, r, app.ID, environment) {
		return
	}

	var in createSupportOrderHTTPInput
	rawBody, ok := readBodyJSON(w, r, h.maxBodyBytes, &in, "Unable to decode request body.")
	if !ok {
		return
	}
	amount, err := amountFromJSON(in.Amount)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Amount must be a number or numeric string.", nil)
		return
	}
	// Fixed-amount buttons dictate the amount; open buttons (or no button)
	// take the buyer-entered figure.
	linkLabel := ""
	if linkID := strings.TrimSpace(in.LinkID); linkID != "" {
		link, found := findSupportLink(data.Settings.Links, linkID)
		if !found || !link.Active {
			httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Unknown support button.", map[string]string{"link_id": "That support button is no longer available."})
			return
		}
		linkLabel = link.Label
		if strings.TrimSpace(link.AmountMode) == "fixed" {
			amount = strings.TrimSpace(link.Amount)
		}
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "TZS"
	}
	message := strings.TrimSpace(in.SupporterMessage)
	if runeCount(message) > 280 {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Supporter message must be 280 characters or fewer.", map[string]string{"supporter_message": "Keep it to 280 characters or fewer."})
		return
	}
	min, max, err := h.service.SupportBounds(r.Context(), app, data.Settings.MinAmount, data.Settings.MaxAmount)
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to check support limits.", err)
		return
	}
	if belowAmount(amount, min) {
		httputil.Error(w, http.StatusUnprocessableEntity, "amount_too_small", "Support amount is below this page's minimum of "+min+" TZS.", map[string]string{"amount": "Minimum is " + min + " TZS."})
		return
	}
	if aboveAmount(amount, max) {
		httputil.Error(w, http.StatusUnprocessableEntity, "amount_too_large", "Support amount is above this page's maximum of "+max+" TZS.", map[string]string{"amount": "Maximum is " + max + " TZS."})
		return
	}

	state, handled := h.checkIdempotency(w, r, app.ID, IdempotencyEndpointOrdersCreate, rawBody)
	if handled {
		return
	}

	// Metadata is built server-side (never client-supplied) so public
	// callers cannot inject provider metadata. The supporter message rides
	// here: visible to the creator in their dashboard, never public.
	metadata := map[string]any{
		"kind":       "support",
		"handle":     data.Org.Handle,
		"link_id":    strings.TrimSpace(in.LinkID),
		"link_label": linkLabel,
	}
	if message != "" {
		metadata["supporter_message"] = message
	}
	order, vErrs, err := h.service.CreateOrder(r.Context(), app, CreatePaymentOrderInput{
		Provider:    in.Provider,
		Amount:      amount,
		Currency:    currency,
		BuyerName:   in.BuyerName,
		BuyerEmail:  in.BuyerEmail,
		BuyerPhone:  in.BuyerPhone,
		Metadata:    metadata,
		Environment: environment,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrLiveTxnCapExceeded):
			h.writeError(state, w, r, http.StatusUnprocessableEntity, "live_txn_cap_exceeded", "Order amount exceeds the live per-transaction cap.", nil)
		case errors.Is(err, ErrLiveDailyCapExceeded):
			h.writeError(state, w, r, http.StatusUnprocessableEntity, "live_daily_cap_exceeded", "Order would exceed the live daily volume cap.", nil)
		default:
			h.failIdempotent(state, w, r, http.StatusBadGateway, "payment_provider_error", "Unable to create support order.", err)
		}
		return
	}
	if vErrs.Any() {
		h.writeError(state, w, r, http.StatusUnprocessableEntity, "validation_failed", "Please check your support payment input.", vErrs)
		return
	}
	h.writeJSON(state, w, r, http.StatusCreated, map[string]any{"data": order})
}

func findSupportLink(links []orgs.SupportLink, id string) (orgs.SupportLink, bool) {
	for _, l := range links {
		if l.ID == id {
			return l, true
		}
	}
	return orgs.SupportLink{}, false
}

func runeCount(s string) int {
	return len([]rune(s))
}

func decimalRat(value string) (*big.Rat, bool) {
	return new(big.Rat).SetString(strings.TrimSpace(value))
}

func belowAmount(amount, min string) bool {
	a, okA := decimalRat(amount)
	m, okM := decimalRat(min)
	if !okA || !okM {
		return true
	}
	return a.Cmp(m) < 0
}

func aboveAmount(amount, max string) bool {
	if strings.TrimSpace(max) == "" {
		return false
	}
	a, okA := decimalRat(amount)
	m, okM := decimalRat(max)
	if !okA || !okM {
		return true
	}
	return a.Cmp(m) > 0
}

// ---------- Merchant management: enable + configure ----------

func merchantClaimsUserID(w http.ResponseWriter, r *http.Request) (string, bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return "", false
	}
	return claims.Subject, true
}

func (h *Handler) supportOrgError(w http.ResponseWriter, err error, action string) {
	switch {
	case errors.Is(err, orgs.ErrOrgNotFound):
		httputil.Error(w, http.StatusNotFound, "not_found", "Organization not found.", nil)
	case errors.Is(err, orgs.ErrNotOrgMember):
		httputil.Error(w, http.StatusForbidden, "forbidden", "You don't have access to this organization.", nil)
	case errors.Is(err, orgs.ErrForbidden):
		httputil.Error(w, http.StatusForbidden, "forbidden", "Your role doesn't allow "+action+".", nil)
	case errors.Is(err, orgs.ErrNotCreatorOrg):
		httputil.Error(w, http.StatusForbidden, "wrong_kind", "Support pages are for creator accounts only.", nil)
	case errors.Is(err, orgs.ErrSupportPageDisabled):
		httputil.Error(w, http.StatusConflict, "support_not_enabled", "This creator is not accepting support yet.", nil)
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to complete the support request.", err)
	}
}

// EnableSupportPage links a receiving app to the creator's public page,
// creating the Support app through the single CreateApp path when none is
// linked, and seeding the default buttons when the page has none.
func (h *Handler) EnableSupportPage(w http.ResponseWriter, r *http.Request) {
	userID, ok := merchantClaimsUserID(w, r)
	if !ok {
		return
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization access is not configured.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	org, err := h.orgs.RequireCreatorOrg(r.Context(), userID, orgID, orgs.PermDevelop)
	if err != nil {
		h.supportOrgError(w, err, "manage the support page")
		return
	}
	if existing, err := h.orgs.GetSupportSettings(r.Context(), userID, orgID); err == nil {
		if _, err := h.service.ResolveSupportApp(r.Context(), orgID, existing.SupportAppID); err == nil {
			httputil.JSON(w, http.StatusOK, map[string]any{"data": existing})
			return
		}
	}
	result, vErrs, err := h.service.CreateApp(r.Context(), CreatePaymentAppInput{
		Name:                  supportAppName(org.DisplayName),
		Description:           "Receiving app for public support payments.",
		OrgID:                 orgID,
		InitialKeyEnvironment: h.initialKeyEnvironment(r, orgID),
	})
	if err != nil {
		h.fail(w, http.StatusInternalServerError, "create_failed", "Unable to create the support app.", err)
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your support app input.", vErrs)
		return
	}
	settings, err := h.orgs.EnsureSupportPage(r.Context(), userID, orgID, result.App.ID)
	if err != nil {
		h.supportOrgError(w, err, "enable the support page")
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": settings})
}

func supportAppName(displayName string) string {
	if name := strings.TrimSpace(displayName); name != "" {
		return name + " Support"
	}
	return "Support"
}

// GetSupportSettings returns the creator's page configuration for the
// Settings editor (any active member may read).
func (h *Handler) GetSupportSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := merchantClaimsUserID(w, r)
	if !ok {
		return
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization access is not configured.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.orgs.RequireCreatorOrg(r.Context(), userID, orgID, orgs.PermRead); err != nil {
		h.supportOrgError(w, err, "view the support page")
		return
	}
	settings, err := h.orgs.GetSupportSettings(r.Context(), userID, orgID)
	if err != nil {
		h.supportOrgError(w, err, "view the support page")
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": settings})
}

type supportLinkHTTPInput struct {
	Label      string `json:"label"`
	AmountMode string `json:"amount_mode"`
	Amount     string `json:"amount"`
}

type supportSettingsHTTPInput struct {
	SupportAppID string                 `json:"support_app_id"`
	MinAmount    string                 `json:"min_amount"`
	MaxAmount    string                 `json:"max_amount"`
	Links        []supportLinkHTTPInput `json:"links"`
}

// UpdateSupportSettings replaces bounds, buttons, and the receiving app
// (owner/manage_org). The app must belong to the org — verified here
// against the org's app list, since the orgs module cannot see apps.
func (h *Handler) UpdateSupportSettings(w http.ResponseWriter, r *http.Request) {
	userID, ok := merchantClaimsUserID(w, r)
	if !ok {
		return
	}
	if h.orgs == nil {
		h.fail(w, http.StatusInternalServerError, "internal_error", "Organization access is not configured.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if _, err := h.orgs.RequireCreatorOrg(r.Context(), userID, orgID, orgs.PermManageOrg); err != nil {
		h.supportOrgError(w, err, "manage the support page")
		return
	}
	var in supportSettingsHTTPInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Unable to decode request body.", nil)
		return
	}
	if appID := strings.TrimSpace(in.SupportAppID); appID != "" {
		if _, err := h.service.ResolveSupportApp(r.Context(), orgID, appID); err != nil {
			httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Receiving app must be one of this organization's apps.", map[string]string{"support_app_id": "Pick one of your apps, or enable the page to create one."})
			return
		}
	}
	links := make([]orgs.SupportLinkInput, 0, len(in.Links))
	for _, l := range in.Links {
		links = append(links, orgs.SupportLinkInput{Label: l.Label, AmountMode: l.AmountMode, Amount: l.Amount})
	}
	settings, vErrs, err := h.orgs.SaveSupportSettings(r.Context(), userID, orgID, orgs.SupportSettingsInput{
		SupportAppID: in.SupportAppID, MinAmount: in.MinAmount, MaxAmount: in.MaxAmount, Links: links,
	})
	if err != nil {
		h.supportOrgError(w, err, "manage the support page")
		return
	}
	if vErrs.Any() {
		httputil.Error(w, http.StatusUnprocessableEntity, "validation_failed", "Please check your support page input.", vErrs)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": settings})
}
