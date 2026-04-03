CREATE TYPE event_status AS ENUM ('upcoming', 'ongoing', 'completed', 'cancelled');
CREATE TYPE reservation_status AS ENUM ('pending', 'confirmed', 'expired', 'cancelled');

-- Users Table
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Events Table
CREATE TABLE events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    date DATE NOT NULL,
    location VARCHAR(255) NOT NULL,
    price NUMERIC(10, 2) NOT NULL,
    start_time TIMESTAMPTZ NOT NULL,
    status event_status NOT NULL DEFAULT 'upcoming',
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Inventory Table
CREATE TABLE inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_id UUID UNIQUE REFERENCES events(id) ON DELETE CASCADE,
    version INT NOT NULL DEFAULT 0,
    available_tickets INT NOT NULL CHECK (available_tickets >= 0) 
);

-- Reservations Table
CREATE TABLE reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    event_id UUID REFERENCES events(id) ON DELETE CASCADE,
    status reservation_status NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL,                            
    created_at TIMESTAMPTZ DEFAULT NOW(),
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

-- Idempotency Keys
CREATE TABLE idempotency_keys (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    key VARCHAR(255) UNIQUE NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Indexes for read performance
CREATE INDEX idx_inventory_event_id ON inventory(event_id);
CREATE INDEX idx_reservations_user_id ON reservations(user_id);
CREATE INDEX idx_reservations_event_id ON reservations(event_id);

---------------------------------------------------------
-- DEMO DATA SEEDING
---------------------------------------------------------

-- 1. Insert Test User (email: test@sentinel.com, password: password123)
-- Using a hardcoded UUID so it's predictable if needed later
INSERT INTO users (id, email, password_hash) 
VALUES (
    '11111111-1111-1111-1111-111111111111', 
    'test@sentinel.com', 
    '$2a$10$vI8aWBnW3fID.ZQ4/zo1G.q1lRps.9cGLcZEiGDMVr5yUP1KUOYTa' 
);

-- 2. Insert Events
-- Using hardcoded UUIDs so we can reliably link the inventory below
INSERT INTO events (id, name, description, date, location, price, start_time, status) 
VALUES 
    (
        '22222222-2222-2222-2222-222222222222', 
        'Coldplay Live', 
        'Music concert featuring all the greatest hits. General admission.', 
        '2026-12-01', 
        'Stadium', 
        5000.00, -- 5000 cents/paise format for the UI
        '2026-12-01 19:30:00Z', 
        'upcoming'
    ),
    (
        '33333333-3333-3333-3333-333333333333', 
        'Tech DevFest', 
        'Annual developer festival with hands-on workshops and networking.', 
        '2026-10-15', 
        'Convention Center', 
        2500.00, 
        '2026-10-15 09:00:00Z', 
        'upcoming'
    );

-- 3. Insert Inventory linked to the Events
INSERT INTO inventory (event_id, version, available_tickets) 
VALUES 
    ('22222222-2222-2222-2222-222222222222', 0, 100),  -- 100 tickets for Coldplay
    ('33333333-3333-3333-3333-333333333333', 0, 500);  -- 500 tickets for DevFest

-- (Reservations and Idempotency Keys are intentionally left blank for a clean testing slate)       
