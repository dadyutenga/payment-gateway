package notifications

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"lipago/internal/platform/auth"
	"lipago/internal/shared/httputil"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) identity(r *http.Request) (userID, adminID string, ok bool) {
	claims, ok := auth.ClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		return "", "", false
	}
	if claims.Audience == auth.AudienceAdmin {
		return "", claims.Subject, true
	}
	return claims.Subject, "", true
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	unread := strings.EqualFold(r.URL.Query().Get("unread"), "true")
	items, total, err := h.service.List(r.Context(), userID, adminID, unread, r.URL.Query().Get("event_type"), limit, offset)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load notifications.", nil)
		return
	}
	count, err := h.service.UnreadCount(r.Context(), userID, adminID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to count notifications.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"total": total, "unread": count, "limit": limit, "offset": offset}})
}

func (h *Handler) MarkRead(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	id := r.PathValue("id")
	if err := h.service.MarkRead(r.Context(), userID, adminID, id); err != nil {
		httputil.Error(w, http.StatusNotFound, "not_found", "Notification was not found.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": id, "read": true}})
}

func (h *Handler) MarkAllRead(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	updated, err := h.service.MarkAllRead(r.Context(), userID, adminID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to update notifications.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": map[string]any{"updated": updated}})
}

type preferenceInput struct {
	ScopeKind string `json:"scope_kind"`
	ScopeID   string `json:"scope_id"`
	EventType string `json:"event_type"`
	Channel   string `json:"channel"`
	Enabled   *bool  `json:"enabled"`
}

func (h *Handler) Preferences(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok {
		httputil.Error(w, http.StatusUnauthorized, "unauthorized", "Missing authenticated user.", nil)
		return
	}
	scopeKind, scopeID := "user", userID
	if r.Method == http.MethodPatch {
		var in preferenceInput
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Enabled == nil {
			httputil.Error(w, http.StatusBadRequest, "invalid_request", "Invalid notification preference.", nil)
			return
		}
		if in.ScopeKind != "" {
			scopeKind = in.ScopeKind
		}
		if in.ScopeID != "" {
			scopeID = in.ScopeID
		}
		if adminID != "" {
			scopeKind, scopeID = "admin", adminID
		}
		if err := h.service.ValidatePreferenceScope(r.Context(), userID, adminID, scopeKind, scopeID, true); err != nil {
			status := http.StatusForbidden
			if strings.Contains(err.Error(), "invalid") {
				status = http.StatusBadRequest
			}
			httputil.Error(w, status, "invalid_preference_scope", err.Error(), nil)
			return
		}
		if err := h.service.SavePreference(r.Context(), scopeKind, scopeID, in.EventType, in.Channel, *in.Enabled); err != nil {
			status := http.StatusBadRequest
			if strings.Contains(err.Error(), "internal") {
				status = http.StatusInternalServerError
			}
			httputil.Error(w, status, "invalid_preference", err.Error(), nil)
			return
		}
	} else {
		if adminID != "" {
			scopeKind, scopeID = "admin", adminID
		} else if requestedKind := strings.TrimSpace(r.URL.Query().Get("scope_kind")); requestedKind != "" {
			scopeKind = requestedKind
			scopeID = strings.TrimSpace(r.URL.Query().Get("scope_id"))
		}
		if err := h.service.ValidatePreferenceScope(r.Context(), userID, adminID, scopeKind, scopeID, false); err != nil {
			status := http.StatusForbidden
			if strings.Contains(err.Error(), "invalid") {
				status = http.StatusBadRequest
			}
			httputil.Error(w, status, "invalid_preference_scope", err.Error(), nil)
			return
		}
	}
	items, err := h.service.ListPreferences(r.Context(), scopeKind, scopeID)
	if err != nil {
		httputil.Error(w, http.StatusInternalServerError, "internal_error", "Unable to load notification preferences.", nil)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": items})
}

type BroadcastRequest struct {
	Title        string         `json:"title"`
	Body         string         `json:"body"`
	Icon         string         `json:"icon"`
	Severity     string         `json:"severity"`
	Target       map[string]any `json:"target"`
	ScheduledFor string         `json:"scheduled_for,omitempty"`
}

func (h *Handler) Broadcast(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		httputil.Error(w, http.StatusForbidden, "forbidden", "Admin access is required.", nil)
		return
	}
	var in BroadcastRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Invalid broadcast payload.", nil)
		return
	}
	id, count, err := h.service.Broadcast(r.Context(), adminID, in)
	if err != nil {
		httputil.Error(w, http.StatusBadRequest, "broadcast_failed", err.Error(), nil)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"id": id, "status": "queued", "recipients": count}})
}

type TargetedRequest struct {
	Title    string `json:"title"`
	Body     string `json:"body"`
	Icon     string `json:"icon"`
	Severity string `json:"severity"`
}

func (h *Handler) Targeted(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		httputil.Error(w, http.StatusForbidden, "forbidden", "Admin access is required.", nil)
		return
	}
	var in TargetedRequest
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "invalid_request", "Invalid notification payload.", nil)
		return
	}
	orgID := r.PathValue("orgID")
	if err := h.service.SendToOrg(r.Context(), adminID, orgID, in); err != nil {
		httputil.Error(w, http.StatusBadRequest, "notification_failed", err.Error(), nil)
		return
	}
	httputil.JSON(w, http.StatusAccepted, map[string]any{"data": map[string]any{"org_id": orgID}})
}
