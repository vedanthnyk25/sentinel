package reservation

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type Service struct {
	db    *database.Queries
	redis *redis.Client
}

type ReservationMessage struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	EventID       uuid.UUID `json:"event_id"`
	UserID        uuid.UUID `json:"user_id"`
}

func NewService(db *database.Queries, redis *redis.Client) *Service {
	return &Service{
		db:    db,
		redis: redis,
	}
}

var reserveScript = redis.NewScript(`
	local idempotency_key = KEYS[1]
	local stock_key = KEYS[2]
	local stream_key = KEYS[3]

	local ttl = ARGV[1]
	local payload = ARGV[2]

	local is_new = redis.call("SETNX", idempotency_key, "1")
	if is_new == 0 then
		return "ErrDuplicateRequest"
	end
	redis.call("EXPIRE", idempotency_key, ttl)

	local stock = tonumber(redis.call("GET", stock_key) or "0")
	if stock <= 0 then
		redis.call("DEL", idempotency_key)
		return "ErrSoldOut"
	end
	redis.call("DECR", stock_key)

	redis.call("XADD", stream_key, "*", "payload", payload)

	return "OK"
`)

func (s *Service) ReserveTicket(ctx context.Context, eventID, userID uuid.UUID, idempotencyKey string) (database.Reservation, error) {

	reservationID := uuid.New()

	msg := ReservationMessage{
		ReservationID: reservationID,
		EventID:       eventID,
		UserID:        userID,
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		return database.Reservation{}, fmt.Errorf("failed to marshal message: %v", err)
	}

	stockKey := fmt.Sprintf("event:%s:stock", eventID.String())
	idemKey := fmt.Sprintf("idempotency:%s", idempotencyKey)
	streamKey := "reservations:stream"

	result, err := reserveScript.Run(ctx, s.redis, []string{idemKey, stockKey, streamKey}, 24*60*60, string(msgBytes)).Result()
	if err != nil {
		return database.Reservation{}, fmt.Errorf("failed to run reserve script: %v", err)
	}

	if result == "ErrDuplicateRequest" {
		return database.Reservation{}, ErrDuplicateRequest
	} else if result == "ErrSoldOut" {
		return database.Reservation{}, ErrSoldOut
	} else if result != "OK" {
		return database.Reservation{}, fmt.Errorf("unexpected result from reserve script: %v", result)
	}

	reservation := database.Reservation{
		ID:        reservationID,
		EventID:   uuid.NullUUID{UUID: eventID, Valid: true},
		UserID:    uuid.NullUUID{UUID: userID, Valid: true},
		Status:    "pending",
		ExpiresAt: time.Now().Add(10 * time.Minute),
	}

	return reservation, nil
}

func (s *Service) GetUserReservations(ctx context.Context, userId uuid.UUID) ([]database.GetUserReservationsRow, error) {
	reservations, err := s.db.GetUserReservations(ctx, uuid.NullUUID{UUID: userId, Valid: true})
	if err != nil {
		return nil, err
	}
	return reservations, nil
}

func (s *Service) ResetInventory(
	ctx context.Context,
	eventID uuid.UUID,
	tickets int32,
) error {

	err := s.db.ResetInventory(
		ctx,
		database.ResetInventoryParams{
			EventID:          uuid.NullUUID{UUID: eventID, Valid: true},
			AvailableTickets: tickets,
		},
	)

	if err != nil {
		return err
	}

	stockKey := fmt.Sprintf(
		"event:%s:stock",
		eventID.String(),
	)

	return s.redis.Set(
		ctx,
		stockKey,
		tickets,
		0,
	).Err()
}

func (s *Service) GetInventory(ctx context.Context, eventID uuid.UUID) (int32, error) {
	inventory, err := s.db.GetInventory(
		ctx,
		uuid.NullUUID{UUID: eventID, Valid: true},
	)
	if err != nil {
		return 0, err
	}
	return inventory.AvailableTickets, nil
}
