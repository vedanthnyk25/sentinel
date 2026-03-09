package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"log"
	"net/http"

	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/broker"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
	"github.com/vedanthnyk25/sentinel/internal/reservation"
	"github.com/vedanthnyk25/sentinel/internal/worker"
)

type ReserveRequest struct {
	UserID  string `json:"user_id"`
	EventID string `json:"event_id"`
}

func main() {
	// Initialize database connection
	dsn := "postgres://root:secretpassword@localhost:5432/sentinel?sslmode=disable"
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("Failed to open DB connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping DB: %v", err)
	}

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to ping Redis: %v", err)
	}
	defer rdb.Close()

	// Initialize RabbitMQ connection
	rmq, err := broker.NewRabbitMQ("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rmq.Conn.Close()
	defer rmq.Chan.Close()

	// Initialize services
	queries := database.New(db)
	reservationService := reservation.NewService(queries, db, rdb, rmq.Chan)

	// Initialize janitor
	janitor := worker.NewJanitor(queries, db, rdb, rmq.Chan)
	janitor.Start()

	http.HandleFunc("/reserve", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		idempotencyKey := r.Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			http.Error(w, "Missing Idempotency-Key header", http.StatusBadRequest)
			return
		}

		var req ReserveRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		userId, err := uuid.Parse(req.UserID)
		if err != nil {
			http.Error(w, "Invalid user_id", http.StatusBadRequest)
			return
		}

		eventId, err := uuid.Parse(req.EventID)
		if err != nil {
			http.Error(w, "Invalid event_id", http.StatusBadRequest)
			return
		}

		res, err := reservationService.ReserveTicket(r.Context(), userId, eventId, idempotencyKey)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			//http.Error(w, "Failed to reserve ticket", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(res)
	})

	log.Println("Sentinel API running on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("Server crashed: %v", err)
	}
}
