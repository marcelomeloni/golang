BEGIN;

ALTER TABLE market_withdraw_requests ADD COLUMN IF NOT EXISTS external_ref text;

COMMIT;
