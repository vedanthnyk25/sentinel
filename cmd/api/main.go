package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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

	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(50)

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

	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	resHandler := reservation.NewHandler(reservationService)
	resHandler.RegisterRoutes(r)

	log.Println("Sentinel API running on http://localhost:8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Server crashed: %v", err)
	}
}
