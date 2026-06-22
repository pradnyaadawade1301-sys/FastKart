-- Migration 004: Reviews, Offers, Notifications, FCM Tokens

-- ── Reviews ───────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS reviews (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    restaurant_id UUID NOT NULL REFERENCES restaurants(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id      UUID REFERENCES orders(id) ON DELETE SET NULL,
    rating        SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, restaurant_id)
);
CREATE INDEX IF NOT EXISTS idx_reviews_restaurant ON reviews(restaurant_id);
CREATE INDEX IF NOT EXISTS idx_reviews_user       ON reviews(user_id);

-- ── Offers / Coupons ──────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS offers (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code           VARCHAR(30) NOT NULL UNIQUE,
    title          VARCHAR(100) NOT NULL,
    description    TEXT,
    discount_type  VARCHAR(10) NOT NULL CHECK (discount_type IN ('percent','flat')),
    discount_value NUMERIC(10,2) NOT NULL,
    min_order      NUMERIC(10,2) NOT NULL DEFAULT 0,
    max_discount   NUMERIC(10,2) NOT NULL DEFAULT 0,
    is_active      BOOLEAN NOT NULL DEFAULT true,
    expires_at     TIMESTAMPTZ NOT NULL,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_offers_code      ON offers(UPPER(code));
CREATE INDEX IF NOT EXISTS idx_offers_active    ON offers(is_active, expires_at);

-- Sample offers
INSERT INTO offers (code, title, description, discount_type, discount_value, min_order, max_discount, expires_at)
VALUES
  ('WELCOME50', 'Pehla Order Special', 'Pehle order pe 50% off — max ₹100', 'percent', 50, 99,  100, NOW() + INTERVAL '30 days'),
  ('FLAT30',    '₹30 Off',             'Kisi bhi order pe ₹30 seedha off',   'flat',    30, 149,  30,  NOW() + INTERVAL '15 days'),
  ('SAVE20',    '20% Cashback',        '₹500+ ke order pe 20% off',          'percent', 20, 500, 150,  NOW() + INTERVAL '7 days')
ON CONFLICT (code) DO NOTHING;

-- ── Notifications ─────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS notifications (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title      VARCHAR(200) NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    type       VARCHAR(50) NOT NULL DEFAULT 'general',
    data       JSONB DEFAULT '{}',
    is_read    BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notifications_user    ON notifications(user_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_notifications_unread  ON notifications(user_id, is_read) WHERE is_read = false;

-- ── FCM Tokens ────────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS fcm_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT NOT NULL,
    platform   VARCHAR(10) NOT NULL DEFAULT 'android',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (user_id, platform)
);