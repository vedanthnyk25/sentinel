package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/auth"
	"github.com/vedanthnyk25/sentinel/internal/catalog"
	mw "github.com/vedanthnyk25/sentinel/internal/middleware"
	"github.com/vedanthnyk25/sentinel/internal/payment"
	"github.com/vedanthnyk25/sentinel/internal/platform/broker"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
	"github.com/vedanthnyk25/sentinel/internal/reservation"
	"github.com/vedanthnyk25/sentinel/internal/worker"
)

func main() {
	// =========================================================================
	// Configuration & Secrets
	// =========================================================================
	dsn := "postgres://root:secretpassword@localhost:5432/sentinel?sslmode=disable"
	JWT_SECRET := "supersecret"

	stripeSecretKey := "sk_test_51SE8SGE8CY15Rz8nW1zlrL7H3KvxGy16NHT2BuBtsY9fQbLlptOmbj9ZDb5NvUHHt9vKGkxUjGpjsSm1kbeuuSwW00fuP1DQ3w"
	stripeWebhookSecret := "whsec_03254fa980adf91b091017c341187c7899663da7858b849a88baa9e52d36652f"

	// =========================================================================
	// Infrastructure Layer (Databases, Caches, Brokers)
	// =========================================================================
	// PostgreSQL
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

	// Redis
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		log.Fatalf("Failed to ping Redis: %v", err)
	}
	defer rdb.Close()

	// RabbitMQ
	rmq, err := broker.NewRabbitMQ("amqp://guest:guest@localhost:5672/")
	if err != nil {
		log.Fatalf("Failed to connect to RabbitMQ: %v", err)
	}
	defer rmq.Conn.Close()
	defer rmq.Chan.Close()

	// =========================================================================
	//  Data Access Layer
	// =========================================================================
	queries := database.New(db)

	// =========================================================================
	// Service Layer (Business Logic)
	// =========================================================================
	authService := auth.NewService(queries, JWT_SECRET)
	catalogService := catalog.NewService(queries, rdb)
	reservationService := reservation.NewService(queries, db, rdb, rmq.Chan)
	paymentService := payment.NewService(queries, stripeSecretKey, stripeWebhookSecret)

	// =========================================================================
	// Handler Layer (HTTP/JSON Parsing)
	// =========================================================================
	authHandler := auth.NewHandler(authService)
	catalogHandler := catalog.NewHandler(catalogService)
	resHandler := reservation.NewHandler(reservationService)
	paymentHandler := payment.NewHandler(paymentService)

	// =========================================================================
	// Background Workers
	// =========================================================================
	janitor := worker.NewJanitor(queries, db, rdb, rmq.Chan)
	janitor.Start()

	// =========================================================================
	// Routing & Middleware
	// =========================================================================
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowOriginFunc: func(r *http.Request, origin string) bool {
			return origin == "http://localhost:3000"
		},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Idempotency-Key"},
		AllowCredentials: true,
	}))

	// Public Routes (No Auth Needed)
	r.Route("/auth", func(r chi.Router) {
		authHandler.RegisterRoutes(r)
	})
	r.Group(func(r chi.Router) {
		catalogHandler.RegisterRoutes(r)
		r.Post("/webhooks/stripe", paymentHandler.HandleWebhook)
	})

	// Protected Routes (Require valid JWT)
	r.Group(func(r chi.Router) {
		r.Use(mw.RequireAuth(JWT_SECRET))
		resHandler.RegisterRoutes(r)
		r.Post("/checkout", paymentHandler.HandleCreateCheckoutSession)
	})

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// =========================================================================
	// Server Startup
	// =========================================================================
	go func() {
		log.Println("Sentinel API running on http://localhost:8080")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server crashed: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	log.Println("Shutting down Sentinel API...")
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Failed to gracefully shutdown: %v", err)
	}

	log.Println("Sentinel API stopped")
}
