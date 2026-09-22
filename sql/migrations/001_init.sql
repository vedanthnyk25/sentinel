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
    id UUID PRIMARY KEY,
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
INSERT INTO users (id, email, password_hash) 
VALUES (
    '11111111-1111-1111-1111-111111111111', 
    'test@sentinel.com', 
    '$2a$10$j5ZPuzBoy2Yz3d5fVqYeGu5CwbFxjNXH9Iec9fYZ8r8VMe3sHzElG'
);

-- 2. Insert Events
INSERT INTO events (id, name, description, date, location, price, start_time, status) 
VALUES 
    (
        '22222222-2222-2222-2222-222222222222', 
        'Coldplay Live', 
        'Music concert featuring all the greatest hits. General admission.', 
        '2026-12-01', 
        'Wembley Stadium, London', 
        5000.00, 
        '2026-12-01 19:30:00Z', 
        'upcoming'
    ),
    (
        '33333333-3333-3333-3333-333333333333', 
        'Tech DevFest', 
        'Annual developer festival with hands-on workshops and networking.', 
        '2026-10-15', 
        'Convention Center, San Jose', 
        2500.00, 
        '2026-10-15 09:00:00Z', 
        'upcoming'
    ),
    (
        '44444444-4444-4444-4444-444444444444', 
        'Taylor Swift | The Eras Tour', 
        'Experience all eras in one unforgettable stadium performance. High demand event.', 
        '2026-11-14', 
        'MetLife Stadium, New Jersey', 
        8500.00, 
        '2026-11-14 18:30:00Z', 
        'upcoming'
    ),
    (
        '55555555-5555-5555-5555-555555555555', 
        'FIFA World Cup Qualifiers', 
        'High-stakes international football clash. Reserved seating and pitchside admission.', 
        '2026-10-22', 
        'Iconic Stadium, Lusail', 
        6000.00, 
        '2026-10-22 20:00:00Z', 
        'upcoming'
    ),
    (
        '66666666-6666-6666-6666-666666666666', 
        'KubeCon + CloudNativeCon', 
        'The premier conference for developers building distributed and scalable cloud systems.', 
        '2026-11-05', 
        'Moscone Center, San Francisco', 
        12000.00, 
        '2026-11-05 08:30:00Z', 
        'upcoming'
    ),
    (
        '77777777-7777-7777-7777-777777777777', 
        'Formula 1 Grand Prix 2026', 
        'Experience high-speed adrenaline from the main grandstand. 3-day weekend access.', 
        '2026-12-12', 
        'Silverstone Circuit, UK', 
        15000.00, 
        '2026-12-12 13:00:00Z', 
        'upcoming'
    ),
    (
        '88888888-8888-8888-8888-888888888888', 
        'EDC Electronic Music Festival', 
        'Three nights of world-renowned DJs, immersive visual stages, and carnival rides.', 
        '2026-11-20', 
        'Motor Speedway, Las Vegas', 
        9500.00, 
        '2026-11-20 17:00:00Z', 
        'upcoming'
    ),
    (
        '99999999-9999-9999-9999-999999999999', 
        'Hans Zimmer Live in Concert', 
        'Epic orchestral performance of iconic film scores including Interstellar and Inception.', 
        '2026-12-18', 
        'Royal Albert Hall, London', 
        7000.00, 
        '2026-12-18 19:30:00Z', 
        'upcoming'
    );

-- 3. Insert Inventory linked to the Events
INSERT INTO inventory (event_id, version, available_tickets) 
VALUES 
    ('22222222-2222-2222-2222-222222222222', 0, 100),
    ('33333333-3333-3333-3333-333333333333', 0, 500),
    ('44444444-4444-4444-4444-444444444444', 0, 200),
    ('55555555-5555-5555-5555-555555555555', 0, 350),
    ('66666666-6666-6666-6666-666666666666', 0, 400),
    ('77777777-7777-7777-7777-777777777777', 0, 150),
    ('88888888-8888-8888-8888-888888888888', 0, 300),
    ('99999999-9999-9999-9999-999999999999', 0, 120);
