package reservation

import "errors"

var (
	ErrDuplicateRequest = errors.New("duplicate request: idempotency key already exists")
	ErrSoldOut          = errors.New("event is sold out")
	ErrRaceCond         = errors.New("reservation failed due to a race condition, please try again")
)
