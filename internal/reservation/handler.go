package reservation

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type ReserveRequest struct {
	UserID  string `json:"user_id"`
	EventID string `json:"event_id"`
}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/reserve", h.handleReserveTicket)
}

func (h *Handler) handleReserveTicket(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if idempotencyKey == "" {
		http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
		return
	}

	var req ReserveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	userID, err := uuid.Parse(req.UserID)
	if err != nil {
		http.Error(w, "Invalid user_id", http.StatusBadRequest)
		return
	}

	eventID, err := uuid.Parse(req.EventID)
	if err != nil {
		http.Error(w, "Invalid event_id", http.StatusBadRequest)
		return
	}

	res, err := h.service.ReserveTicket(r.Context(), userID, eventID, idempotencyKey)
	if err != nil {
		switch {
		case errors.Is(err, ErrDuplicateRequest):
			http.Error(w, err.Error(), http.StatusConflict) // 409
		case errors.Is(err, ErrSoldOut):
			http.Error(w, err.Error(), http.StatusConflict) // 409
		case errors.Is(err, ErrRaceCond):
			http.Error(w, "Server busy, please retry", http.StatusServiceUnavailable) // 503
		default:
			http.Error(w, "Internal server error", http.StatusInternalServerError) // 500
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(res)
}
