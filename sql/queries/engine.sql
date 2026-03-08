-- name: GetInventory :one
-- Retrieves the current inventory for a given event.
SELECT id, event_id, version,  available_tickets
FROM inventory
WHERE event_id = $1;

-- name: UpdateInventoryAtomic :execrows
-- Updates the inventory for a given event atomically.
UPDATE inventory
SET version = version + 1,
    available_tickets = available_tickets - $2
WHERE event_id = $1 
AND version = $3
AND available_tickets >= $2;

-- name: CreateReservation :one
-- Creates a new reservation for a user and event.
INSERT INTO reservations (user_id, event_id, status, expires_at)
VALUES ($1, $2, 'pending', NOW() + INTERVAL '10 minutes')
returning *;

-- name: InsertIdempotencyKey :one
-- Inserts a new idempotency key for a user.
INSERT INTO idempotency_keys (user_id, key)
VALUES ($1, $2)
ON CONFLICT (key) DO NOTHING
RETURNING id;
