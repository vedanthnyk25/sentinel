-- name: GetReservation :one
-- Retrieves a reservation by its ID, including event details.
SELECT r.id AS reservation_id, r.status AS reservation_status, r.expires_at,
        e.id AS event_id, e.name AS event_name,
        e.price AS event_price,
        e.status AS event_status
FROM reservations r
JOIN events e ON r.event_id = e.id
WHERE r.id = $1;

-- name: UpdateReservationStatus :exec
-- Updates the status of a reservation.
UPDATE reservations
SET status = $2, updated_at = NOW()
WHERE id = $1;


-- name: GetUserReservations :many
-- Retrieves all reservations for a specific user, including event details.
SELECT r.id AS reservation_id, r.status AS reservation_status, r.expires_at,
        e.id AS event_id, e.name AS event_name,
        e.price AS event_price,
        e.location AS event_location,
        e.date AS event_date
FROM reservations r
JOIN events e ON r.event_id = e.id
WHERE r.user_id = $1
ORDER BY r.created_at DESC;        
