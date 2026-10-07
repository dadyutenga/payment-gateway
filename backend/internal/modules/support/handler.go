package support

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"lipago/internal/platform/auth"
	"lipago/internal/platform/middleware"
	"lipago/internal/shared/httputil"

	"github.com/google/uuid"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service: service} }

func (h *Handler) identity(r *http.Request) (userID, adminID string, ok bool) {
	claims, ok := middleware.ClaimsFromContext(r.Context())
	if !ok || strings.TrimSpace(claims.Subject) == "" {
		return "", "", false
	}
	if claims.Audience == auth.AudienceAdmin {
		return "", claims.Subject, true
	}
	return claims.Subject, "", true
}

func (h *Handler) fail(w http.ResponseWriter, status int, code, message string) {
	httputil.Error(w, status, code, message, nil)
}

func (h *Handler) mapError(w http.ResponseWriter, err error) {
	var validation *ValidationError
	switch {
	case errors.Is(err, ErrTicketNotFound), errors.Is(err, ErrMessageNotFound):
		h.fail(w, http.StatusNotFound, "not_found", "Support ticket or attachment was not found.")
	case errors.Is(err, ErrTicketForbidden):
		h.fail(w, http.StatusForbidden, "forbidden", "You don't have access to this support ticket.")
	case errors.As(err, &validation):
		h.fail(w, http.StatusUnprocessableEntity, "validation_failed", validation.Message)
	case errors.Is(err, ErrStorageUnavailable):
		h.fail(w, http.StatusServiceUnavailable, "storage_unavailable", "File storage is not configured.")
	default:
		h.fail(w, http.StatusInternalServerError, "internal_error", "Unable to complete the support request.")
	}
}

func (h *Handler) customerOrgID(r *http.Request, userID string) (string, error) {
	if orgID := strings.TrimSpace(r.PathValue("orgID")); orgID != "" {
		return orgID, nil
	}
	return h.service.resolveIndividual(r.Context(), userID)
}

func parseListFilter(r *http.Request) ListFilter {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))
	return ListFilter{Status: r.URL.Query().Get("status"), Category: r.URL.Query().Get("category"), Priority: r.URL.Query().Get("priority"), Queue: r.URL.Query().Get("queue"), Sort: r.URL.Query().Get("sort"), Limit: limit, Offset: offset}
}

func (h *Handler) CustomerList(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok || adminID != "" {
		h.fail(w, http.StatusUnauthorized, "unauthorized", "Customer authentication is required.")
		return
	}
	orgID, err := h.customerOrgID(r, userID)
	if err != nil {
		h.mapError(w, err)
		return
	}
	items, total, err := h.service.ListTickets(r.Context(), userID, "", orgID, parseListFilter(r))
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"total": total}})
}

func (h *Handler) CustomerCreate(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok || adminID != "" {
		h.fail(w, http.StatusUnauthorized, "unauthorized", "Customer authentication is required.")
		return
	}
	orgID, err := h.customerOrgID(r, userID)
	if err != nil {
		h.mapError(w, err)
		return
	}
	var in CreateInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_request", "Invalid support ticket payload.")
		return
	}
	ticket, err := h.service.CreateTicket(r.Context(), userID, orgID, in)
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": ticket})
}

func (h *Handler) CustomerGet(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok || adminID != "" {
		h.fail(w, http.StatusUnauthorized, "unauthorized", "Customer authentication is required.")
		return
	}
	ticket, err := h.service.GetTicket(r.Context(), userID, "", r.PathValue("ticketID"))
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": ticket})
}

func (h *Handler) CustomerReply(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok || adminID != "" {
		h.fail(w, http.StatusUnauthorized, "unauthorized", "Customer authentication is required.")
		return
	}
	var in MessageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_request", "Invalid support message payload.")
		return
	}
	ticket, err := h.service.AddCustomerReply(r.Context(), userID, r.PathValue("ticketID"), in.Body)
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": ticket})
}

func (h *Handler) CustomerClose(w http.ResponseWriter, r *http.Request) {
	userID, adminID, ok := h.identity(r)
	if !ok || adminID != "" {
		h.fail(w, http.StatusUnauthorized, "unauthorized", "Customer authentication is required.")
		return
	}
	ticket, err := h.service.CloseByCustomer(r.Context(), userID, r.PathValue("ticketID"))
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": ticket})
}

func (h *Handler) AdminList(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		h.fail(w, http.StatusForbidden, "forbidden", "Admin access is required.")
		return
	}
	items, total, err := h.service.ListTickets(r.Context(), "", adminID, "", parseListFilter(r))
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": items, "meta": map[string]any{"total": total}})
}

func (h *Handler) AdminGet(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		h.fail(w, http.StatusForbidden, "forbidden", "Admin access is required.")
		return
	}
	ticket, err := h.service.GetTicket(r.Context(), "", adminID, r.PathValue("ticketID"))
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": ticket})
}

func (h *Handler) AdminReply(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		h.fail(w, http.StatusForbidden, "forbidden", "Admin access is required.")
		return
	}
	var in MessageInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_request", "Invalid support message payload.")
		return
	}
	ticket, err := h.service.AddAdminReply(r.Context(), adminID, r.PathValue("ticketID"), in)
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": ticket})
}

func (h *Handler) AdminUpdate(w http.ResponseWriter, r *http.Request) {
	_, adminID, ok := h.identity(r)
	if !ok || adminID == "" {
		h.fail(w, http.StatusForbidden, "forbidden", "Admin access is required.")
		return
	}
	var in AdminUpdate
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_request", "Invalid support update payload.")
		return
	}
	ticket, err := h.service.UpdateByAdmin(r.Context(), adminID, r.PathValue("ticketID"), in)
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusOK, map[string]any{"data": ticket})
}

func (h *Handler) upload(w http.ResponseWriter, r *http.Request, admin bool) {
	userID, adminID, ok := h.identity(r)
	if !ok || (admin && adminID == "") || (!admin && adminID != "") {
		h.fail(w, http.StatusForbidden, "forbidden", "You don't have access to this support ticket.")
		return
	}
	ticketID := r.PathValue("ticketID")
	if _, err := uuid.Parse(ticketID); err != nil {
		h.fail(w, http.StatusNotFound, "not_found", "Support ticket was not found.")
		return
	}
	if admin {
		if _, err := h.service.GetTicket(r.Context(), "", adminID, ticketID); err != nil {
			h.mapError(w, err)
			return
		}
	} else if _, err := h.service.GetTicket(r.Context(), userID, "", ticketID); err != nil {
		h.mapError(w, err)
		return
	}
	const maxSize int64 = 5 << 20
	r.Body = http.MaxBytesReader(w, r.Body, maxSize+1<<20)
	if err := r.ParseMultipartForm(maxSize + 1<<20); err != nil {
		h.fail(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "Attachment exceeds the 5 MB limit.")
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		h.fail(w, http.StatusUnprocessableEntity, "invalid_attachment", "Attach exactly one file.")
		return
	}
	header := files[0]
	file, err := header.Open()
	if err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_attachment", "Unable to read attachment.")
		return
	}
	defer file.Close()
	if header.Size < 0 || header.Size > 5<<20 {
		h.fail(w, http.StatusRequestEntityTooLarge, "attachment_too_large", "Attachment exceeds the 5 MB limit.")
		return
	}
	sample := make([]byte, 512)
	n, readErr := file.Read(sample)
	if readErr != nil && readErr != io.EOF {
		h.fail(w, http.StatusBadRequest, "invalid_attachment", "Unable to read attachment.")
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		h.fail(w, http.StatusBadRequest, "invalid_attachment", "Unable to read attachment.")
		return
	}
	contentType := http.DetectContentType(sample[:n])
	attachment, err := h.service.StoreAttachment(r.Context(), ticketID, filepath.Base(header.Filename), contentType, header.Size, file)
	if err != nil {
		h.mapError(w, err)
		return
	}
	ticket, err := h.service.AddAttachmentMessage(r.Context(), userID, adminID, ticketID, r.FormValue("body"), attachment)
	if err != nil {
		h.mapError(w, err)
		return
	}
	httputil.JSON(w, http.StatusCreated, map[string]any{"data": ticket})
}

func (h *Handler) CustomerUpload(w http.ResponseWriter, r *http.Request) { h.upload(w, r, false) }
func (h *Handler) AdminUpload(w http.ResponseWriter, r *http.Request)    { h.upload(w, r, true) }

func (h *Handler) download(w http.ResponseWriter, r *http.Request, admin bool) {
	userID, adminID, ok := h.identity(r)
	if !ok || (admin && adminID == "") || (!admin && adminID != "") {
		h.fail(w, http.StatusForbidden, "forbidden", "You don't have access to this attachment.")
		return
	}
	attachment, err := h.service.Attachment(r.Context(), userID, adminID, r.PathValue("ticketID"), r.PathValue("messageID"))
	if err != nil {
		h.mapError(w, err)
		return
	}
	object, err := h.service.GetAttachment(r.Context(), attachment)
	if err != nil {
		h.mapError(w, err)
		return
	}
	defer object.Body.Close()
	w.Header().Set("Content-Type", attachment.ContentType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(filepath.Base(attachment.Name), `"`, "")+`"`)
	if object.Info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(object.Info.Size, 10))
	}
	_, _ = io.Copy(w, object.Body)
}

func (h *Handler) CustomerDownload(w http.ResponseWriter, r *http.Request) { h.download(w, r, false) }
func (h *Handler) AdminDownload(w http.ResponseWriter, r *http.Request)    { h.download(w, r, true) }
