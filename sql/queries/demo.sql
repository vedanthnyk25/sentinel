-- name: ResetInventory :exec
UPDATE inventory
SET available_tickets = $2,
    version = 1
WHERE event_id = $1;

