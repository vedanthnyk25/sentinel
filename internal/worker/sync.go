package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/rabbitmq/amqp091-go"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

const (
	streamName   = "reservations:stream"
	groupName    = "sync_group"
	consumerName = "worker-1"

	maxOCCRetries = 3
	occRetryBase  = 20 * time.Millisecond

	// Crash recovery: reclaim messages that have sat unacked longer than
	// this, on the assumption their original consumer died mid-flight.
	reclaimMinIdle  = 30 * time.Second
	reclaimInterval = 15 * time.Second
	reclaimBatch    = 100

	// Poison-message guard: after this many delivery attempts, stop
	// retrying and dead-letter it instead of looping forever.
	maxDeliveries = 5
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
	err := w.redis.XGroupCreateMkStream(ctx, streamName, groupName, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Fatalf("Failed to create Redis consumer group: %v", err)
	}

	// Reclaim anything left over from a previous crash before we start
	// taking new work, then keep sweeping periodically in case *this*
	// process hangs or dies mid-message later.
	w.reclaimStale(ctx)

	go func() {
		ticker := time.NewTicker(reclaimInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				w.reclaimStale(ctx)
			}
		}
	}()

	go func() {
		log.Println("SyncWorker started: listening to Redis stream...")
		for {
			select {
			case <-ctx.Done():
				return
			default:
				streams, err := w.redis.XReadGroup(ctx, &redis.XReadGroupArgs{
					Group:    groupName,
					Consumer: consumerName,
					Streams:  []string{streamName, ">"},
					Count:    10,
					Block:    2 * time.Second,
				}).Result()

				if err != nil {
					if err == redis.Nil {
						continue
					}
					log.Printf("Error reading from Redis stream: %v", err)
					time.Sleep(1 * time.Second)
					continue
				}

				for _, stream := range streams {
					for _, msg := range stream.Messages {
						w.handle(ctx, msg)
					}
				}
			}
		}
	}()
}

// reclaimStale finds messages that have been pending longer than
// reclaimMinIdle — meaning whichever consumer read them never ACKed,
// most likely because it crashed between XADD and XACK — and either
// reprocesses them or dead-letters them if they've been retried too many
// times already.
func (w *SyncWorker) reclaimStale(ctx context.Context) {
	pending, err := w.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: streamName,
		Group:  groupName,
		Start:  "-",
		End:    "+",
		Count:  reclaimBatch,
	}).Result()
	if err != nil {
		if err != redis.Nil {
			log.Printf("reclaimStale: XPendingExt failed: %v", err)
		}
		return
	}

	for _, p := range pending {
		if p.Idle < reclaimMinIdle {
			continue // still plausibly being processed by a live consumer
		}

		if p.RetryCount >= maxDeliveries {
			log.Printf("reclaimStale: message %s exceeded %d delivery attempts, dead-lettering", p.ID, maxDeliveries)
			w.deadLetter(ctx, p.ID)
			continue
		}

		claimed, err := w.redis.XClaim(ctx, &redis.XClaimArgs{
			Stream:   streamName,
			Group:    groupName,
			Consumer: consumerName,
			MinIdle:  reclaimMinIdle,
			Messages: []string{p.ID},
		}).Result()
		if err != nil {
			log.Printf("reclaimStale: XClaim failed for %s: %v", p.ID, err)
			continue
		}

		log.Printf("reclaimStale: reclaimed message %s (idle %s, delivery attempt %d)", p.ID, p.Idle, p.RetryCount+1)
		for _, msg := range claimed {
			w.handle(ctx, msg)
		}
	}
}

// deadLetter gives up on a message that has been retried too many times.
// We can't safely assume whether its Postgres write ever succeeded, so we
// do NOT touch inventory here — only log it loudly for manual inspection
// and ACK it so it stops consuming delivery attempts forever.
func (w *SyncWorker) deadLetter(ctx context.Context, msgID string) {
	log.Printf("DEAD LETTER: message %s abandoned after %d delivery attempts — needs manual investigation", msgID, maxDeliveries)
	w.redis.XAck(ctx, streamName, groupName, msgID)
}

func (w *SyncWorker) handle(ctx context.Context, msg redis.XMessage) {
	payloadStr, ok := msg.Values["payload"].(string)
	if !ok {
		log.Printf("Invalid payload in stream message %s, dead-lettering", msg.ID)
		w.deadLetter(ctx, msg.ID)
		return
	}

	var reserveMsg ReservationMessage
	if err := json.Unmarshal([]byte(payloadStr), &reserveMsg); err != nil {
		log.Printf("Failed to unmarshal stream message %s: %v, dead-lettering", msg.ID, err)
		w.deadLetter(ctx, msg.ID)
		return
	}

	switch w.processMessage(ctx, reserveMsg) {
	case outcomeSuccess:
		w.publishToJanitor(ctx, payloadStr)
		w.redis.XAck(ctx, streamName, groupName, msg.ID)

	case outcomeTerminal:
		// Genuinely can't complete this reservation (OCC exhausted after
		// retries). Give the ticket back and stop retrying.
		stockKey := fmt.Sprintf("event:%s:stock", reserveMsg.EventID.String())
		w.redis.Incr(context.Background(), stockKey)
		w.redis.XAck(ctx, streamName, groupName, msg.ID)
		log.Printf("Reservation %s failed terminally, stock restored", reserveMsg.ReservationID)

	case outcomeTransient:
		// Infra hiccup (DB down, connection error, etc). Don't ACK —
		// leave it in the PEL so reclaimStale (or this same consumer's
		// next read of its own pending list, once we add that) picks it
		// back up. Don't touch stock: the ticket is still legitimately
		// reserved, just not synced to Postgres yet.
		log.Printf("Reservation %s hit a transient failure, leaving pending for retry", reserveMsg.ReservationID)
	}
}

type ReservationMessage struct {
	ReservationID uuid.UUID `json:"reservation_id"`
	UserID        uuid.UUID `json:"user_id"`
	EventID       uuid.UUID `json:"event_id"`
}

type outcome int

const (
	outcomeSuccess outcome = iota
	outcomeTerminal
	outcomeTransient
)

// processMessage performs the actual Postgres write for one reservation,
// with a bounded retry loop around the optimistic-concurrency update.
func (w *SyncWorker) processMessage(ctx context.Context, reserveMsg ReservationMessage) outcome {
	var lastErr error

	for attempt := 0; attempt < maxOCCRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(occRetryBase * time.Duration(1<<uint(attempt-1))) // 20ms, 40ms, 80ms...
		}

		result, err := w.attemptWrite(ctx, reserveMsg)
		switch result {
		case outcomeSuccess:
			return outcomeSuccess
		case outcomeTransient:
			// DB connectivity/tx error — not a conflict, retrying
			// immediately won't help; let reclaimStale retry it later
			// instead of busy-looping here.
			return outcomeTransient
		case outcomeTerminal:
			// OCC version conflict (rows == 0): worth retrying a few
			// times immediately since it should resolve fast.
			lastErr = err
			continue
		}
	}

	log.Printf("Reservation %s: OCC conflict persisted after %d attempts: %v", reserveMsg.ReservationID, maxOCCRetries, lastErr)
	return outcomeTerminal
}

// attemptWrite runs exactly one Postgres transaction attempt.
func (w *SyncWorker) attemptWrite(ctx context.Context, reserveMsg ReservationMessage) (outcome, error) {
	tx, err := w.dbTx.BeginTx(ctx, nil)
	if err != nil {
		return outcomeTransient, fmt.Errorf("begin tx: %w", err)
	}

	success := false
	defer func() {
		if !success {
			tx.Rollback()
		}
	}()

	qtx := w.db.WithTx(tx)

	inventory, err := qtx.GetInventory(ctx, uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true})
	if err != nil {
		return outcomeTransient, fmt.Errorf("get inventory: %w", err)
	}

	rows, err := qtx.UpdateInventoryAtomic(ctx, database.UpdateInventoryAtomicParams{
		EventID:          uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true},
		Version:          inventory.Version,
		AvailableTickets: 1,
	})
	if err != nil {
		return outcomeTransient, fmt.Errorf("update inventory: %w", err)
	}
	if rows == 0 {
		// Someone else updated the row between our read and write.
		// Not a real error, not a reason to give up yet — the retry
		// loop in processMessage will re-read and try again.
		return outcomeTerminal, errors.New("optimistic concurrency conflict")
	}

	_, err = qtx.CreateReservation(ctx, database.CreateReservationParams{
		ID:      reserveMsg.ReservationID,
		UserID:  uuid.NullUUID{UUID: reserveMsg.UserID, Valid: true},
		EventID: uuid.NullUUID{UUID: reserveMsg.EventID, Valid: true},
	})
	if err != nil {
		// A duplicate-key conflict here (redelivery of an already-committed
		// message) is effectively success — the row already exists — so
		// treat unique-violation specially rather than retrying forever.
		if isUniqueViolation(err) {
			tx.Rollback()
			return outcomeSuccess, nil
		}
		return outcomeTransient, fmt.Errorf("create reservation: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return outcomeTransient, fmt.Errorf("commit: %w", err)
	}

	success = true
	return outcomeSuccess, nil
}

func isUniqueViolation(err error) bool {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return true
	}
	return false
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
