package reservation

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type Service struct {
	db    *database.Queries
	dbTx  *sql.DB
	redis *redis.Client
	amqp  *amqp091.Channel
}

type ReservationMessage struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	EventID       uuid.UUID `json:"event_id"`
}

func NewService(db *database.Queries, dbTx *sql.DB, redis *redis.Client, amqp *amqp091.Channel) *Service {
	return &Service{
		db:    db,
		dbTx:  dbTx,
		redis: redis,
		amqp:  amqp,
	}
}

func (s *Service) ReserveTicket(ctx context.Context, userId, eventId uuid.UUID, idempotencyKey string) (database.Reservation, error) {
	// Idempotency check
	_, err := s.db.InsertIdempotencyKey(ctx, database.InsertIdempotencyKeyParams{
		UserID: uuid.NullUUID{UUID: userId, Valid: true},
		Key:    idempotencyKey,
	})
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return database.Reservation{}, ErrDuplicateRequest
		}
		return database.Reservation{}, err
	}

	stockKey := fmt.Sprintf("event:%s:stock", eventId.String())

	remainingStock, err := s.redis.Decr(ctx, stockKey).Result()
	if err != nil {
		return database.Reservation{}, err
	}

	if remainingStock < 0 {
		// Rollback Redis decrement
		s.redis.Incr(ctx, stockKey)
		return database.Reservation{}, ErrSoldOut
	}

	// Start Transaction
	tx, err := s.dbTx.BeginTx(ctx, nil)
	if err != nil {
		// Rollback Redis decrement
		s.redis.Incr(context.Background(), stockKey)
		return database.Reservation{}, err
	}

	success := false
	defer func() {
		if !success {
			tx.Rollback()
			s.redis.Incr(context.Background(), stockKey)
		}
	}()

	qtx := s.db.WithTx(tx)

	inventory, err := qtx.GetInventory(ctx, uuid.NullUUID{UUID: eventId, Valid: true})
	if err != nil {
		return database.Reservation{}, err // Notice no manual tx.Rollback() needed!
	}

	if inventory.AvailableTickets <= 0 {
		return database.Reservation{}, ErrSoldOut
	}

	// Atomic Update
	rows, err := qtx.UpdateInventoryAtomic(ctx, database.UpdateInventoryAtomicParams{
		EventID:          uuid.NullUUID{UUID: eventId, Valid: true},
		Version:          inventory.Version,
		AvailableTickets: inventory.AvailableTickets - 1, // Note: we subtract $1 in SQL, so you might just pass 1 here depending on how you wrote the SQL query!
	})
	if err != nil {
		return database.Reservation{}, err
	}
	if rows == 0 {
		return database.Reservation{}, ErrRaceCond
	}

	// Create Reservation
	reservationRow, err := qtx.CreateReservation(ctx, database.CreateReservationParams{
		UserID:  uuid.NullUUID{UUID: userId, Valid: true},
		EventID: uuid.NullUUID{UUID: eventId, Valid: true},
	})
	if err != nil {
		return database.Reservation{}, err
	}

	if err = tx.Commit(); err != nil {
		return database.Reservation{}, err
	}

	success = true

	reserveMessage := ReservationMessage{
		ReservationID: reservationRow.ID,
		EventID:       eventId,
	}

	msgBytes, err := json.Marshal(reserveMessage)
	if err != nil {
		fmt.Printf("Failed to marshal reservation message: %v\n", err)
	}

	err = s.amqp.PublishWithContext(ctx,
		"",
		"reservations.pending",
		false,
		false,
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			Body:         msgBytes,
		},
	)

	reservation := database.Reservation{
		ID:        reservationRow.ID,
		UserID:    uuid.NullUUID{UUID: userId, Valid: true},
		EventID:   uuid.NullUUID{UUID: eventId, Valid: true},
		Status:    reservationRow.Status,
		ExpiresAt: reservationRow.ExpiresAt,
	}
	return reservation, nil
}
