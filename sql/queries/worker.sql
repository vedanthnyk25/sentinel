-- name: GetReservationStatus :one
SELECT status FROM reservations WHERE id = $1;

-- name: MarkReservationExpired :execrows
UPDATE reservations 
SET status = 'expired', updated_at = NOW() 
WHERE id = $1 AND status = 'pending';

-- name: RefundPostgresInventory :exec
UPDATE inventory 
SET available_tickets = available_tickets + 1 
WHERE event_id = $1;
