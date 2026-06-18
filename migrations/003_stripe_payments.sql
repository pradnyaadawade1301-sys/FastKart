-- migrations/003_stripe_payments.sql

CREATE TABLE IF NOT EXISTS stripe_transactions (
    id                  UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID            NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    order_id            UUID,
    payment_intent_id   VARCHAR(100)    NOT NULL UNIQUE,
    amount              DECIMAL(10,2)   NOT NULL,
    currency            VARCHAR(10)     NOT NULL DEFAULT 'inr',
    status              VARCHAR(20)     NOT NULL DEFAULT 'pending',
    created_at          TIMESTAMP       NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMP       NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_stripe_txn_user_id           ON stripe_transactions(user_id);
CREATE INDEX IF NOT EXISTS idx_stripe_txn_order_id          ON stripe_transactions(order_id);
CREATE INDEX IF NOT EXISTS idx_stripe_txn_payment_intent_id ON stripe_transactions(payment_intent_id);
CREATE INDEX IF NOT EXISTS idx_stripe_txn_status            ON stripe_transactions(status);