package reservation

import (
	"context"
	"database/sql"
	"errors"

	"github.com/google/uuid"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type Service struct {
	db   *database.Queries
	dbTx *sql.DB
	// RabbitMQ connection and channel would go here for the cleanup worker
	// In-memory cache (e.g., Redis client) would go here for idempotency keys
}

func NewService(db *database.Queries, dbTx *sql.DB) *Service {
	return &Service{
		db:   db,
		dbTx: dbTx,
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

	// Start Transaction
	tx, err := s.dbTx.BeginTx(ctx, nil)
	if err != nil {
		return database.Reservation{}, err
	}
	defer tx.Rollback()

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

	reservation := database.Reservation{
		ID:        reservationRow.ID,
		UserID:    uuid.NullUUID{UUID: userId, Valid: true},
		EventID:   uuid.NullUUID{UUID: eventId, Valid: true},
		Status:    reservationRow.Status,
		ExpiresAt: reservationRow.ExpiresAt,
	}
	return reservation, nil
}
