-- name: GetAllEvents :many
-- Retrieves a list of all events in the catalog.
SELECT id, name, description, date, location, price, start_time, status
FROM events
ORDER BY date ASC;

-- name: GetEventByID :one
-- Retrieves a single event by its ID.
SELECT id, name, description, date, location, price, start_time, status
FROM events
WHERE id = $1;
