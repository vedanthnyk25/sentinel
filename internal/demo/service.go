package demo

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/vedanthnyk25/sentinel/internal/reservation"
)

type Service struct {
	reservationService *reservation.Service
}

func NewService(reservationService *reservation.Service) *Service {
	return &Service{
		reservationService: reservationService,
	}
}

type FlashSaleResult struct {
	Buyers             int `json:"buyers"`
	Success            int `json:"success"`
	SoldOut            int `json:"sold_out"`
	RaceConditions     int `json:"race_conditions"`
	Errors             int `json:"errors"`
	InventoryRemaining int `json:"inventory_remaining"`
	TotalTickets       int `json:"total_tickets"`
}

func (s *Service) RunFlashSale(
	ctx context.Context,
	requests int,
	eventID uuid.UUID,
	resetTickets *int32,
) (FlashSaleResult, error) {

	var initialInventory int32
	if resetTickets != nil && *resetTickets > 0 {
		if err := s.ResetInventory(ctx, eventID, *resetTickets); err != nil {
			return FlashSaleResult{}, fmt.Errorf("failed to reset inventory: %w", err)
		}
		initialInventory = *resetTickets
	} else {
		inv, err := s.reservationService.GetInventory(ctx, eventID)
		if err != nil {
			return FlashSaleResult{}, fmt.Errorf("failed to get inventory: %w", err)
		}
		initialInventory = inv
	}

	const workerCount = 1000

	jobs := make(chan struct{}, requests)

	var wg sync.WaitGroup

	var success atomic.Int64
	var soldOut atomic.Int64
	var race atomic.Int64
	var errs atomic.Int64

	demoUserID := uuid.MustParse(
		"11111111-1111-1111-1111-111111111111",
	)

	worker := func() {
		defer wg.Done()

		for range jobs {

			_, err := s.reservationService.ReserveTicket(
				ctx,
				eventID,
				demoUserID,
				uuid.NewString(),
			)

			switch {

			case err == nil:
				success.Add(1)

			case errors.Is(err, reservation.ErrSoldOut):
				soldOut.Add(1)

			case errors.Is(err, reservation.ErrRaceCond):
				race.Add(1)

			default:
				fmt.Printf(
					"unexpected error: %v\n",
					err,
				)

				errs.Add(1)
			}
		}
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go worker()
	}

	for i := 0; i < requests; i++ {
		jobs <- struct{}{}
	}

	close(jobs)

	wg.Wait()

	succCount := int(success.Load())
	rem := int(initialInventory) - succCount
	if rem < 0 {
		rem = 0
	}

	return FlashSaleResult{
		Buyers:             requests,
		Success:            succCount,
		SoldOut:            int(soldOut.Load()),
		RaceConditions:     int(race.Load()),
		Errors:             int(errs.Load()),
		InventoryRemaining: rem,
		TotalTickets:       int(initialInventory),
	}, nil
}

func (s *Service) ResetInventory(ctx context.Context, eventID uuid.UUID, tickets int32) error {
	return s.reservationService.ResetInventory(ctx, eventID, tickets)
}
