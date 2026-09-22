package demo

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Post("/flash-sale", h.handleFlashSale)
	r.Post("/reset-inventory", h.handleResetInventory)
}

type FlashSaleRequest struct {
	EventID      string `json:"event_id"`
	Buyers       int    `json:"buyers"`
	ResetTickets *int32 `json:"reset_tickets,omitempty"`
}

func (h *Handler) handleFlashSale(
	w http.ResponseWriter,
	r *http.Request,
) {
	var req FlashSaleRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	eventID, err := uuid.Parse(req.EventID)
	if err != nil {
		http.Error(w, "invalid event id", http.StatusBadRequest)
		return
	}

	result, err := h.service.RunFlashSale(
		r.Context(),
		req.Buyers,
		eventID,
		req.ResetTickets,
	)

	if err != nil {
		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set(
		"Content-Type",
		"application/json",
	)

	if err := json.NewEncoder(w).Encode(result); err != nil {
		http.Error(
			w,
			"failed to encode response",
			http.StatusInternalServerError,
		)
		return
	}
}

func (h *Handler) handleResetInventory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EventID string `json:"event_id"`
		Tickets int32  `json:"tickets"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	eventID, err := uuid.Parse(req.EventID)
	if err != nil {
		http.Error(w, "invalid event id", http.StatusBadRequest)
		return
	}
	if err := h.service.ResetInventory(r.Context(), eventID, req.Tickets); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}
