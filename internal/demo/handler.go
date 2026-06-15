package demo

import (
	"encoding/json"
	"net/http"

	"github.com/containerd/log"
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
}

type FlashSaleRequest struct {
	EventID string `json:"event_id"`
	Buyers  int    `json:"buyers"`
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

	if err:= json.NewEncoder(w).Encode(result); err != nil {
		http.Error(
			w,
			"failed to encode response",
			http.StatusInternalServerError,
		)
		return
	}
}
