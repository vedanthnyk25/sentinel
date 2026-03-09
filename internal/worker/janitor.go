package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type Janitor struct {
	db    *database.Queries
	dbTx  *sql.DB
	redis *redis.Client
	amqp  *amqp091.Channel
}

func NewJanitor(db *database.Queries, dbTx *sql.DB, redis *redis.Client, amqp *amqp091.Channel) *Janitor {
	return &Janitor{
		db:    db,
		dbTx:  dbTx,
		redis: redis,
		amqp:  amqp,
	}
}

func (j *Janitor) Start() {
	msgs, err := j.amqp.Consume(
		"reservations.expired",
		"",
		false,
		false,
		false,
		false,
		nil,
	)
	if err != nil {
		log.Fatalf("Janitor failed to connect to queue: %v", err)
	}

	go func() {
		for msg := range msgs {
			j.processExpiredMessage(msg)
		}
	}()
}

func (j *Janitor) processExpiredMessage(d amqp091.Delivery) {
	ctx := context.Background()

	// Parse message
	var msg struct {
		ReservationID uuid.UUID `json:"reservation_id"`
		EventID       uuid.UUID `json:"event_id"`
	}
	err := json.Unmarshal(d.Body, &msg)
	if err != nil {
		log.Printf("Janitor failed to unmarshal message: %v", err)
		d.Reject(false)
		return
	}

	// Check if reservation is still pending
	status, err := j.db.GetReservationStatus(ctx, msg.ReservationID)
	if err != nil {
		log.Printf("Janitor failed to get reservation status: %v", err)
		d.Reject(false)
		return
	}

	if status == "confirmed" {
		log.Printf("Janitor skipping reservation %s as it is already confirmed", msg.ReservationID)
		d.Ack(false)
		return
	}

	tx, _ := j.dbTx.BeginTx(ctx, nil)

	defer tx.Rollback()
	qtx := j.db.WithTx(tx)

	rows, err := qtx.MarkReservationExpired(ctx, msg.ReservationID)
	if err != nil || rows == 0 {
		log.Printf("Janitor failed to mark reservation %s as expired", msg.ReservationID)
		d.Nack(false, true)
		return
	}

	if err := qtx.RefundPostgresInventory(ctx, uuid.NullUUID{UUID: msg.EventID, Valid: true}); err != nil {
		log.Printf("Janitor failed to refund inventory for event %s: %v", msg.EventID, err)
		d.Nack(false, true)
		return
	}

	err = tx.Commit()
	if err != nil {
		log.Printf("Janitor failed to commit transaction: %v", err)
		d.Nack(false, true)
		return
	}

	// Refund Redis stock
	stockKey := fmt.Sprintf("event:%s:stock", msg.EventID.String())
	err = j.redis.Incr(ctx, stockKey).Err()
	if err != nil {
		log.Printf("Janitor failed to refund Redis stock for event %s: %v", msg.EventID, err)
		d.Nack(false, true)
		return
	}
	d.Nack(false, false)
}
