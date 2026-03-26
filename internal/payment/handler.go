package payment

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/google/uuid"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) HandleCreateCheckoutSession(w http.ResponseWriter, r *http.Request) {

	var req struct {
		ReservationID string `json:"reservation_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	reservationID, err := uuid.Parse(req.ReservationID)
	if err != nil {
		http.Error(w, "Invalid reservation ID", http.StatusBadRequest)
		return
	}

	url, err := h.svc.CreateCheckoutSession(r.Context(), reservationID)
	if err != nil {
		http.Error(w, "Failed to create checkout session", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"url": url})
}

func (h *Handler) HandleWebhook(w http.ResponseWriter, r *http.Request) {
	payload, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read webhook payload", http.StatusBadRequest)
		return
	}

	// Process the webhook payload
	stripeHeader := r.Header.Get("Stripe-Signature")
	if err := h.svc.HandleWebhook(r.Context(), payload, stripeHeader); err != nil {
		http.Error(w, "Failed to process webhook", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusOK)
}
