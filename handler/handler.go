package handler

import (
	"alerts/alerts"
	"alerts/service"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
)

// Handler is the HTTP layer for alert endpoints.
type Handler struct {
	svc *service.AlertService
}

func NewHandler(svc *service.AlertService) *Handler {
	return &Handler{svc: svc}
}

// createAlertRequest is the request body for POST /alerts.
type createAlertRequest struct {
	TransactionID     string `json:"transaction_id"`
	MatchedEntityName string `json:"matched_entity_name"`
	MatchScore        int    `json:"match_score"`
}

// decisionRequest is the request body for POST /alerts/{id}/decision.
type decisionRequest struct {
	Status       alerts.Status `json:"status"`
	DecisionNote string        `json:"decisionNote"`
}

// CreateAlert handles POST /alerts.
//
// It expects:
//   - context value    : tenantID set by upstream auth middleware
//   - request body     : { "transaction_id": "...", "matched_entity_name": "...", "match_score": 0-100 }
//
// Responses:
//
//	201  created Alert JSON with server-generated id, status=OPEN, timestamps
//	400  malformed body or invalid field values
//	403  tenantID absent from context (middleware gap)
//	500  unexpected store error
func (h *Handler) CreateAlert(w http.ResponseWriter, r *http.Request) {
	// --- tenant from context (set by auth middleware) ---
	tenantID, err := TenantIDFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusForbidden, "tenant identity required")
		return
	}

	// --- decode body ---
	var req createAlertRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// --- delegate to service ---
	alert, err := h.svc.Create(r.Context(), tenantID, req.TransactionID, req.MatchedEntityName, req.MatchScore)
	if err != nil {
		switch {
		case errors.Is(err, alerts.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusCreated, alert)
}

// ListAlerts handles GET /alerts.
//
// It expects:
//   - context value    : tenantID set by upstream auth middleware
//   - query params     : status (optional), minMatchScore (optional, 0-100)
//
// Responses:
//
//	200  array of Alert JSON (may be empty)
//	400  invalid query parameters
//	403  tenantID absent from context (middleware gap)
//	500  unexpected store error
func (h *Handler) ListAlerts(w http.ResponseWriter, r *http.Request) {
	// --- tenant from context (set by auth middleware) ---
	tenantID, err := TenantIDFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusForbidden, "tenant identity required")
		return
	}

	// --- parse optional filters from query params ---
	var status *alerts.Status
	if statusStr := r.URL.Query().Get("status"); statusStr != "" {
		s := alerts.Status(statusStr)
		status = &s
	}

	var minMatchScore *int
	if scoreStr := r.URL.Query().Get("minMatchScore"); scoreStr != "" {
		score, err := strconv.Atoi(scoreStr)
		if err != nil || score < 0 || score > 100 {
			writeError(w, http.StatusBadRequest, "invalid minMatchScore: must be integer 0-100")
			return
		}
		minMatchScore = &score
	}

	// --- delegate to service ---
	log.Printf("ListAlerts: tenant=%q, status=%v, minMatchScore=%v", tenantID, status, minMatchScore)
	alerts, err := h.svc.List(r.Context(), tenantID, status, minMatchScore)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Return empty array instead of null for consistency
	if alerts == nil {
		log.Printf("ListAlerts: no alerts found for tenant %q", tenantID)
		writeJSON(w, http.StatusOK, "{}")
	}

	writeJSON(w, http.StatusOK, alerts)
}

// EscalateAlert handles POST /alerts/{id}/escalate.
//
// It expects:
//   - path parameter   : alert ID
//   - context value    : tenantID set by upstream auth middleware
//
// Responses:
//
//	200  updated Alert JSON with status=ESCALATED
//	400  invalid request or invalid status transition
//	403  tenantID absent from context (middleware gap)
//	404  alert not found for this tenant
//	500  unexpected store error
func (h *Handler) EscalateAlert(w http.ResponseWriter, r *http.Request) {
	// --- tenant from context (set by auth middleware) ---
	tenantID, err := TenantIDFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusForbidden, "tenant identity required")
		return
	}

	// --- path param: alert ID ---
	alertID := r.PathValue("id")
	if alertID == "" {
		writeError(w, http.StatusBadRequest, "missing alert id in path")
		return
	}

	log.Printf("EscalateAlert: tenant=%q, alertID=%q", tenantID, alertID)
	// --- delegate to service ---
	alert, event, err := h.svc.Escalate(r.Context(), tenantID, alertID)
	if err != nil {
		switch {
		case errors.Is(err, alerts.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, alerts.ErrNotFound):
			writeError(w, http.StatusNotFound, "alert not found")
		case errors.Is(err, alerts.ErrInvalidStatus):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	h.svc.EmitAlert(r.Context(), alert) // best-effort async emit; errors logged internally
	log.Printf("Escalation event emitted: %+v", event)

	writeJSON(w, http.StatusOK, alert)
}

// SubmitDecision handles POST /alerts/{id}/decision.
//
// It expects:
//   - path parameter   : alert ID (via chi/mux — see RegisterRoutes)
//   - context value    : tenantID set by upstream auth middleware
//   - request body     : { "status": "CLEARED"|"CONFIRMED_HIT", "decisionNote": "..." }
//
// Responses:
//
//	200  updated Alert JSON
//	400  malformed body or invalid field values
//	403  tenantID absent from context (middleware gap)
//	404  alert not found for this tenant
//	409  alert already decided
//	500  unexpected store error
func (h *Handler) SubmitDecision(w http.ResponseWriter, r *http.Request) {
	// --- tenant from context (set by auth middleware) ---
	tenantID, err := TenantIDFromContext(r.Context())
	if err != nil {
		writeError(w, http.StatusForbidden, "tenant identity required")
		return
	}

	// --- path param: alert ID ---
	// Using net/http 1.22+ pattern variables; swap PathValue for chi/gorilla as needed.
	alertID := r.PathValue("id")
	if alertID == "" {
		writeError(w, http.StatusBadRequest, "missing alert id in path")
		return
	}

	// --- decode body ---
	var req decisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// --- delegate to service ---
	alert, err := h.svc.SubmitDecision(r.Context(), tenantID, alertID, req.Status, req.DecisionNote)
	if err != nil {
		switch {
		case errors.Is(err, alerts.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, alerts.ErrNotFound):
			writeError(w, http.StatusNotFound, "alert not found")
		case errors.Is(err, alerts.ErrAlreadyDecided):
			writeError(w, http.StatusConflict, "alert already decided")
		default:
			writeError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	writeJSON(w, http.StatusOK, alert)
}

// RegisterRoutes wires the handler into a *http.ServeMux (stdlib 1.22+).
// Swap for chi.Router or *gin.Engine as preferred.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /alerts", h.ListAlerts)
	mux.HandleFunc("POST /alerts", h.CreateAlert)
	mux.HandleFunc("POST /alerts/{id}/escalate", h.EscalateAlert)
	mux.HandleFunc("POST /alerts/{id}/decision", h.SubmitDecision)
}

// --- helpers ---

type errorResponse struct {
	Error string `json:"error"`
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, errorResponse{Error: msg})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
