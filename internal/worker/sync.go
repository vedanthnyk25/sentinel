package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type SyncWorker struct {
	db    *database.Queries
	dbTx  *sql.DB
	redis *redis.Client
	amqp  *amqp091.Channel
}

func NewSyncWorker(db *database.Queries, dbTx *sql.DB, redis *redis.Client, amqp *amqp091.Channel) *SyncWorker {
	return &SyncWorker{db: db, dbTx: dbTx, redis: redis, amqp: amqp}
}

func (w *SyncWorker) Start(ctx context.Context) {
	streamName := "reservations:stream"
	groupName := "sync_group"

	// Ensure the Consumer Group exists
	err := w.redis.XGroupCreateMkStream(ctx, streamName, groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Fatalf("Failed to create Redis consumer group: %v", err)
	}

	go func() {
		log.Println("SyncWorker started: listening to Redis stream...")
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// Read from the stream, blocking for up to 2 seconds
				streams, err := w.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    groupName,
					Consumer: "worker-1", // Update if scaling horizontally
					Streams:  []string{streamName, ">"},
					Count:    10, // Batch size
					Block:    2 * time.Second,
				}).Result()

				if err != nil {
					if err == redis.Nil {
						continue // Timeout, no new messages
					}
					log.Printf("Error reading from Redis stream: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}

				for _, stream := range streams {
					for _, msg := range stream.Messages {
						w.processMessage(ctx, streamName, groupName, msg)
					}
				}
			}
		}
	}()
}

func (w *SyncWorker) processMessage(ctx context.Context, stream, group string, msg redis.XMessage) {
	payloadStr, ok := msg.Values["payload"].(string)
	if !ok {
		log.Printf("Invalid payload in stream message %s", msg.ID)
		w.redis.XAck(ctx, stream, group, msg.ID)
		return
	}

	var reserveMsg struct {
		ReservationID uuid.UUID `json:"reservation_id"`
		UserID        uuid.UUID `json:"user_id"`
		EventID       uuid.UUID `json:"event_id"`
	}

	if err := json.Unmarshal([]byte(payloadStr), &reserveMsg); err != nil {
		log.Printf("Failed to unmarshal stream message: %v", err)
		w.redis.XAck(ctx, stream, group, msg.ID)
		return
	}

	// Begin Postgres Transaction
	tx, err := w.dbTx.BeginTx(ctx, nil)
	if err != nil {
		log.Printf("Failed to begin tx: %v", err)
		return // Return without ACKing so it stays in the Pending Entries List for retry
	}

	success := false
	defer func() {
		if !success {
			tx.Rollback()
			// COMPENSATING ACTION: Restore Redis stock if Postgres fails
			stockKey := fmt.Sprintf("event:%s:stock", reserveMsg.EventID.String())
			w.redis.Incr(context.Background(), stockKey)

			// ACK so we don't infinitely retry a doomed database constraint
			w.redis.XAck(context.Background(), stream, group, msg.ID)
		}
	}()

	qtx := w.db.WithTx(tx)

	inventory, err := qtx.GetInventory(ctx, uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true})
	if err != nil {
		log.Printf("Failed to get Postgres inventory: %v", err)
		return
	}

	rows, err := qtx.UpdateInventoryAtomic(ctx, database.UpdateInventoryAtomicParams{
		EventID:          uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true},
		Version:          inventory.Version,
		AvailableTickets: 1,
	})

	if err != nil || rows == 0 {
		log.Printf("Worker failed atomic update (OCC conflict or error)")
		return
	}

	_, err = qtx.CreateReservation(ctx, database.CreateReservationParams{
		ID:      reserveMsg.ReservationID,
		UserID:  uuid.NullUUID{UUID: reserveMsg.UserID, Valid: true},
		EventID: uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true},
	})
	if err != nil {
		log.Printf("Worker failed to insert reservation: %v", err)
		return
	}

	if err := tx.Commit(); err != nil {
		log.Printf("Worker failed to commit tx: %v", err)
		return
	}

	success = true

	// Publish to RabbitMQ for Janitor expiration
	w.publishToJanitor(ctx, payloadStr)

	// Acknowledge the message in Redis
	w.redis.XAck(ctx, stream, group, msg.ID)
}

func (w *SyncWorker) publishToJanitor(ctx context.Context, payload string) {
	err := w.amqp.PublishWithContext(ctx,
		"",
		"reservations.pending",
		false,
		false,
		amqp091.Publishing{
			ContentType:  "application/json",
			DeliveryMode: amqp091.Persistent,
			Body:         []byte(payload),
		},
	)
	if err != nil {
		log.Printf("Warning: Failed to hand off to RabbitMQ Janitor: %v", err)
	}
}
