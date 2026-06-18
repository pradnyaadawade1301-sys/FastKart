-- FastKart Database Schema
-- Run: psql -U postgres -d fastkart -f migrations/001_init.sql

-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS earthdistance CASCADE;

-- ── Users ─────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS users (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    phone            VARCHAR(15) UNIQUE NOT NULL,
    name             VARCHAR(100),
    email            VARCHAR(100),
    avatar_url       TEXT,
    role             VARCHAR(20) DEFAULT 'customer',
    wallet_balance   DECIMAL(10,2) DEFAULT 0,
    points           INT DEFAULT 0,
    default_address  TEXT,
    is_active        BOOLEAN DEFAULT true,
    created_at       TIMESTAMP DEFAULT NOW(),
    updated_at       TIMESTAMP DEFAULT NOW()
);

-- ── OTP Logs ──────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS otp_logs (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    phone      VARCHAR(15) NOT NULL,
    otp        VARCHAR(6)  NOT NULL,
    is_used    BOOLEAN DEFAULT false,
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_otp_phone ON otp_logs(phone);

-- ── Addresses ─────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS addresses (
    id         UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id    UUID REFERENCES users(id) ON DELETE CASCADE,
    label      VARCHAR(50) DEFAULT 'Home',
    name       VARCHAR(100),
    phone      VARCHAR(15),
    line1      TEXT NOT NULL,
    city       VARCHAR(100),
    pincode    VARCHAR(10),
    lat        DECIMAL(9,6),
    lng        DECIMAL(9,6),
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMP DEFAULT NOW()
);

-- ── Restaurants ───────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS restaurants (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name          VARCHAR(200) NOT NULL,
    image_url     TEXT,
    cover_url     TEXT,
    rating        DECIMAL(2,1) DEFAULT 4.0,
    review_count  INT DEFAULT 0,
    delivery_time VARCHAR(20) DEFAULT '30-40 min',
    delivery_fee  DECIMAL(8,2) DEFAULT 20,
    min_order     DECIMAL(8,2) DEFAULT 100,
    distance      VARCHAR(20),
    is_open       BOOLEAN DEFAULT true,
    categories    TEXT[],
    tags          TEXT[],
    address       TEXT,
    lat           DECIMAL(9,6),
    lng           DECIMAL(9,6),
    description   TEXT,
    owner_id      UUID REFERENCES users(id),
    created_at    TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_restaurant_categories ON restaurants USING GIN(categories);

-- ── Food Items ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS food_items (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    restaurant_id UUID REFERENCES restaurants(id) ON DELETE CASCADE,
    name          VARCHAR(200) NOT NULL,
    description   TEXT,
    image_url     TEXT,
    price         DECIMAL(8,2) NOT NULL,
    original_price DECIMAL(8,2) DEFAULT 0,
    category      VARCHAR(100),
    is_popular    BOOLEAN DEFAULT false,
    is_new        BOOLEAN DEFAULT false,
    is_available  BOOLEAN DEFAULT true,
    rating        DECIMAL(2,1) DEFAULT 4.0,
    sold_count    INT DEFAULT 0,
    created_at    TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_food_restaurant ON food_items(restaurant_id);

-- ── Drivers ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS drivers (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID REFERENCES users(id),
    name        VARCHAR(100),
    phone       VARCHAR(15),
    image_url   TEXT,
    vehicle_no  VARCHAR(20),
    is_active   BOOLEAN DEFAULT true,
    is_online   BOOLEAN DEFAULT false,
    current_lat DECIMAL(9,6),
    current_lng DECIMAL(9,6),
    rating      DECIMAL(2,1) DEFAULT 4.5,
    created_at  TIMESTAMP DEFAULT NOW()
);

-- ── Orders ────────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS orders (
    id               UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id          UUID REFERENCES users(id),
    restaurant_id    UUID REFERENCES restaurants(id),
    restaurant_name  VARCHAR(200),
    restaurant_image TEXT,
    driver_id        UUID REFERENCES drivers(id),
    items            JSONB NOT NULL,
    subtotal         DECIMAL(10,2),
    delivery_fee     DECIMAL(8,2),
    discount         DECIMAL(8,2) DEFAULT 0,
    platform_fee     DECIMAL(8,2) DEFAULT 0,
    taxes            DECIMAL(8,2) DEFAULT 0,
    total            DECIMAL(10,2),
    coupon_code      VARCHAR(50),
    status           VARCHAR(30) DEFAULT 'placed',
    payment_method   VARCHAR(30) DEFAULT 'cash',
    payment_status   VARCHAR(20) DEFAULT 'pending',
    delivery_address JSONB,
    otp              VARCHAR(6),
    estimated_delivery TIMESTAMP,
    delivered_at     TIMESTAMP,
    cancelled_at     TIMESTAMP,
    cancel_reason    TEXT,
    created_at       TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_order_user ON orders(user_id);
CREATE INDEX IF NOT EXISTS idx_order_status ON orders(status);
CREATE INDEX IF NOT EXISTS idx_order_restaurant ON orders(restaurant_id);

-- ── Wallet Transactions ───────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS wallet_transactions (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID REFERENCES users(id),
    type        VARCHAR(20),  -- credit / debit
    amount      DECIMAL(10,2),
    description TEXT,
    reference   VARCHAR(100),
    created_at  TIMESTAMP DEFAULT NOW()
);

-- ── Reviews ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS reviews (
    id            UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id       UUID REFERENCES users(id),
    restaurant_id UUID REFERENCES restaurants(id),
    order_id      UUID REFERENCES orders(id),
    rating        INT CHECK(rating BETWEEN 1 AND 5),
    comment       TEXT,
    created_at    TIMESTAMP DEFAULT NOW()
);

-- ── Coupons ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS coupons (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    code            VARCHAR(50) UNIQUE NOT NULL,
    type            VARCHAR(20),  -- percent / flat
    value           DECIMAL(8,2),
    min_order       DECIMAL(8,2) DEFAULT 0,
    max_discount    DECIMAL(8,2),
    usage_limit     INT DEFAULT 1,
    used_count      INT DEFAULT 0,
    expires_at      TIMESTAMP,
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- ── Sample Data ───────────────────────────────────────────────────────────
INSERT INTO restaurants (name, image_url, cover_url, rating, review_count,
    delivery_time, delivery_fee, min_order, is_open, categories, tags,
    address, lat, lng, description)
VALUES
('Punjabi Tadka',
 'https://images.unsplash.com/photo-1585937421612-70a008356fbe?w=400&h=300&fit=crop',
 'https://images.unsplash.com/photo-1585937421612-70a008356fbe?w=800&h=400&fit=crop',
 4.8, 2341, '25-35 min', 20, 150, true,
 ARRAY['North Indian','Dal Makhani'], ARRAY['Popular','Free Delivery'],
 '12 Connaught Place, New Delhi', 28.6315, 77.2167,
 'Authentic Punjabi food made with pure ghee and fresh spices.'),
('Biryani Bros',
 'https://images.unsplash.com/photo-1563379091339-03b21ab4a4f8?w=400&h=300&fit=crop',
 'https://images.unsplash.com/photo-1563379091339-03b21ab4a4f8?w=800&h=400&fit=crop',
 4.5, 987, '20-30 min', 30, 120, true,
 ARRAY['Biryani','Hyderabadi'], ARRAY['Spicy','Best Biryani'],
 '78 Banjara Hills, Hyderabad', 17.4126, 78.4480,
 'Slow-cooked dum biryani with authentic Hyderabadi spices.')
ON CONFLICT DO NOTHING;

INSERT INTO coupons (code, type, value, min_order, max_discount, usage_limit, expires_at)
VALUES
('FIRST50',  'percent', 50, 100, 200, 1,  NOW() + INTERVAL '30 days'),
('SAVE10',   'flat',    10, 50,  10,  100, NOW() + INTERVAL '30 days'),
('FREESHIP', 'flat',    20, 0,   20,  100, NOW() + INTERVAL '30 days')
ON CONFLICT DO NOTHING;