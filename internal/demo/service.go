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
}

func (s *Service) RunFlashSale(
	ctx context.Context,
	requests int,
	eventID uuid.UUID,
) (FlashSaleResult, error) {

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
				demoUserID,
				eventID,
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

	remainingInventory, _ :=
		s.reservationService.GetInventory(
			ctx,
			eventID,
		)

	return FlashSaleResult{
		Buyers:             requests,
		Success:            int(success.Load()),
		SoldOut:            int(soldOut.Load()),
		RaceConditions:     int(race.Load()),
		Errors:             int(errs.Load()),
		InventoryRemaining: int(remainingInventory),
	}, nil
}

func (s *Service) ResetInventory(ctx context.Context, eventID uuid.UUID, tickets int32) error {
	err := s.reservationService.ResetInventory(ctx, eventID, tickets)
	return err
}
