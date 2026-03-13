package catalog

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/vedanthnyk25/sentinel/internal/platform/database"
)

type Service struct {
	db  *database.Queries
	rdb *redis.Client
}

func NewService(db *database.Queries, rdb *redis.Client) *Service {
	return &Service{
		db:  db,
		rdb: rdb,
	}
}

func (s *Service) ListEvents(ctx context.Context) ([]database.Event, error) {
	events, err := s.db.GetAllEvents(ctx)
	if err != nil {
		return []database.Event{}, err
	}

	var result []database.Event
	for _, event := range events {
		result = append(result, database.Event{
			ID:          event.ID,
			Name:        event.Name,
			Description: event.Description,
			Date:        event.Date,
			Price:       event.Price,
			Location:    event.Location,
			StartTime:   event.StartTime,
		})
	}
	return result, nil
}

func (s *Service) GetEventByID(ctx context.Context, eventID string) (database.Event, error) {
	id, err := uuid.Parse(eventID)
	if err != nil {
		return database.Event{}, err
	}

	redisKey := "cache:event:" + eventID

	// Try to get it from Redis
	cachedEvent, err := s.rdb.Get(ctx, redisKey).Result()

	if err == nil {
		var eventData database.Event
		if unmarshalErr := json.Unmarshal([]byte(cachedEvent), &eventData); unmarshalErr == nil {
			return eventData, nil
		}
	}

	// In either case, we query Postgres. This makes the system resilient to Redis failures.
	dbEvent, err := s.db.GetEventByID(ctx, id)
	if err != nil {
		return database.Event{}, err
	}

	finalEventJSON, err := json.Marshal(dbEvent)
	if err == nil {
		_ = s.rdb.Set(ctx, redisKey, finalEventJSON, 5*time.Minute).Err()
	}

	return database.Event{
		ID:          dbEvent.ID,
		Name:        dbEvent.Name,
		Description: dbEvent.Description,
		Date:        dbEvent.Date,
		Price:       dbEvent.Price,
		Location:    dbEvent.Location,
		StartTime:   dbEvent.StartTime,
	}, nil
}
