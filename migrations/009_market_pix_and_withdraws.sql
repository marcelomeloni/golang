BEGIN;

ALTER TABLE orders ADD COLUMN IF NOT EXISTS pix_e2e_id text;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS pix_paid_amount numeric(12,2);
ALTER TABLE orders ADD COLUMN IF NOT EXISTS pix_paid_at timestamptz;

CREATE UNIQUE INDEX IF NOT EXISTS idx_orders_pix_e2e_id
    ON orders(pix_e2e_id)
    WHERE pix_e2e_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS market_withdraw_requests (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    seller_id    uuid NOT NULL REFERENCES users(id),
    amount       numeric(12,2) NOT NULL,
    pix_key      text NOT NULL,
    pix_key_type text,
    status       text NOT NULL DEFAULT 'pending',
    admin_note   text,
    reviewed_by  uuid REFERENCES users(id),
    reviewed_at  timestamptz,
    paid_at      timestamptz,
    created_at   timestamptz NOT NULL DEFAULT NOW(),
    updated_at   timestamptz NOT NULL DEFAULT NOW()
);

ALTER TABLE market_transactions ADD COLUMN IF NOT EXISTS withdraw_request_id uuid REFERENCES market_withdraw_requests(id);

CREATE INDEX IF NOT EXISTS idx_market_withdraw_requests_seller ON market_withdraw_requests(seller_id);
CREATE INDEX IF NOT EXISTS idx_market_withdraw_requests_status ON market_withdraw_requests(status);

COMMIT;
