package payment

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/google/uuid"
	"github.com/stripe/stripe-go/v84"
	"github.com/stripe/stripe-go/v84/checkout/session"
	"github.com/stripe/stripe-go/v84/webhook"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

var ErrInvalidReservationStatus = errors.New("invalid reservation status: expected 'pending'")

type Service struct {
	db                  *database.Queries
	stripeSecretKey     string
	stripeWebhookSecret string
}

func NewService(db *database.Queries, stripeSecretKey, stripeWebhookSecret string) *Service {
	stripe.Key = stripeSecretKey

	return &Service{
		db:                  db,
		stripeSecretKey:     stripeSecretKey,
		stripeWebhookSecret: stripeWebhookSecret,
	}
}

func (s *Service) CreateCheckoutSession(ctx context.Context, reservationID uuid.UUID) (string, error) {
	reservation, err := s.db.GetReservation(ctx, reservationID)
	if err != nil {
		return "", err
	}

	if reservation.ReservationStatus != "pending" {
		return "", ErrInvalidReservationStatus
	}

	priceFloat, err := strconv.ParseFloat(reservation.EventPrice, 64)
	if err != nil {
		return "", err
	}
	priceInCents := int64(priceFloat * 100)

	params := &stripe.CheckoutSessionParams{
		PaymentMethodTypes: stripe.StringSlice([]string{"card"}),
		ClientReferenceID:  stripe.String(reservationID.String()),
		LineItems: []*stripe.CheckoutSessionLineItemParams{
			{
				PriceData: &stripe.CheckoutSessionLineItemPriceDataParams{
					Currency: stripe.String("usd"),
					ProductData: &stripe.CheckoutSessionLineItemPriceDataProductDataParams{
						Name: stripe.String(reservation.EventName),
					},
					UnitAmount: stripe.Int64(priceInCents),
				},
				Quantity: stripe.Int64(1),
			},
		},
		Mode:       stripe.String(string(stripe.CheckoutSessionModePayment)),
		SuccessURL: stripe.String("http://localhost:3000/success"),
		CancelURL:  stripe.String("http://localhost:3000/cancel"),
	}

	sess, err := session.New(params)
	if err != nil {
		return "", err
	}

	return sess.URL, nil
}

func (s *Service) HandleWebhook(ctx context.Context, payload []byte, signatureHeader string) error {
	event, err := webhook.ConstructEvent(payload, signatureHeader, s.stripeWebhookSecret)
	if err != nil {
		return err
	}

	if event.Type == "checkout.session.completed" {
		var sess stripe.CheckoutSession
		err := json.Unmarshal(event.Data.Raw, &sess)
		if err != nil {
			return err
		}

		reservationId, err := uuid.Parse(sess.ClientReferenceID)
		if err != nil {
			return err
		}

		err = s.db.UpdateReservationStatus(ctx, database.UpdateReservationStatusParams{
			ID:     reservationId,
			Status: "confirmed",
		})
		if err != nil {
			return err
		}
	}
	return nil
}
