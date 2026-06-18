-- migrations/002_warehouse.sql
-- Run: psql -U postgres -d fastkart -f migrations/002_warehouse.sql

-- ── Warehouse (inventory store) ───────────────────────────────────────────
CREATE TABLE IF NOT EXISTS warehouse (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name            VARCHAR(200) NOT NULL,       -- e.g. "Mumbai Central Warehouse"
    city            VARCHAR(100),
    address         TEXT,
    lat             DECIMAL(9,6),
    lng             DECIMAL(9,6),
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMP DEFAULT NOW()
);

-- ── Inventory (har item ka stock) ────────────────────────────────────────
CREATE TABLE IF NOT EXISTS inventory (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    warehouse_id    UUID REFERENCES warehouse(id) ON DELETE CASCADE,
    food_item_id    UUID REFERENCES food_items(id) ON DELETE CASCADE,
    quantity        INT DEFAULT 0,
    min_quantity    INT DEFAULT 10,       -- below this = low stock alert
    unit            VARCHAR(20) DEFAULT 'pcs',
    last_restocked  TIMESTAMP DEFAULT NOW(),
    updated_at      TIMESTAMP DEFAULT NOW(),
    UNIQUE(warehouse_id, food_item_id)
);

-- ── Order Warehouse Log (har order ka warehouse record) ──────────────────
CREATE TABLE IF NOT EXISTS order_warehouse_log (
    id              UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id        UUID REFERENCES orders(id) ON DELETE CASCADE,
    warehouse_id    UUID REFERENCES warehouse(id),
    status          VARCHAR(30) DEFAULT 'received',
    -- received → picking → packed → dispatched → delivered
    picked_at       TIMESTAMP,
    packed_at       TIMESTAMP,
    dispatched_at   TIMESTAMP,
    delivered_at    TIMESTAMP,
    notes           TEXT,
    created_at      TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_owl_order ON order_warehouse_log(order_id);
CREATE INDEX IF NOT EXISTS idx_owl_warehouse ON order_warehouse_log(warehouse_id);

-- ── Payment Transactions ──────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS payment_transactions (
    id                  UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    order_id            UUID REFERENCES orders(id),
    user_id             UUID REFERENCES users(id),
    razorpay_order_id   VARCHAR(100),
    razorpay_payment_id VARCHAR(100),
    razorpay_signature  VARCHAR(300),
    amount              DECIMAL(10,2),
    currency            VARCHAR(10) DEFAULT 'INR',
    status              VARCHAR(20) DEFAULT 'pending',
    -- pending / paid / failed / refunded
    method              VARCHAR(30),
    created_at          TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_pt_order ON payment_transactions(order_id);

-- ── Notifications ────────────────────────────────────────────────────────
CREATE TABLE IF NOT EXISTS notifications (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id     UUID REFERENCES users(id),
    title       VARCHAR(200),
    body        TEXT,
    type        VARCHAR(50),   -- order_update / promo / delivery
    ref_id      UUID,          -- order_id ya koi bhi reference
    is_read     BOOLEAN DEFAULT false,
    created_at  TIMESTAMP DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_notif_user ON notifications(user_id);

-- ── Sample Warehouse Data ─────────────────────────────────────────────────
INSERT INTO warehouse (name, city, address, lat, lng) VALUES
('Mumbai Central Warehouse', 'Mumbai',    'Dharavi Industrial Area, Mumbai',      19.0422, 72.8549),
('Delhi NCR Warehouse',      'New Delhi', 'Okhla Industrial Estate, New Delhi',   28.5355, 77.2500),
('Bangalore Hub',            'Bangalore', 'Electronic City Phase 1, Bangalore',   12.8399, 77.6770),
('Hyderabad Warehouse',      'Hyderabad', 'Gachibowli Industrial Area, Hyderabad',17.4400, 78.3489)
ON CONFLICT DO NOTHING;