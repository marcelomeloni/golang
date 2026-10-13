-- ============================================================================
-- 006_payout_requests.sql
-- Pedidos de repasse: a organização solicita o pagamento (PIX) do saldo já
-- liberado do escrow; o admin da plataforma vê, paga manualmente e marca como
-- pago (ou rejeita).
--
-- O saldo disponível NÃO fica nesta tabela: ele é derivado de orders
-- (status='paid') liberadas 7 dias após events.end_date. Aqui guardamos apenas
-- o histórico das solicitações e um snapshot da conta PIX usada no momento do
-- pedido, para o repasse não mudar se a conta da org for editada depois.
--
-- Idempotente.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS payout_requests (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    requested_by    uuid REFERENCES users(id),
    amount          numeric(12,2) NOT NULL,
    status          text NOT NULL DEFAULT 'pending',  -- pending|paid|rejected|cancelled
    bank_account_id uuid REFERENCES organization_bank_accounts(id),
    pix_key         text,
    pix_key_type    text,
    holder_name     text,
    holder_document text,
    note            text,        -- observação da organização
    admin_note      text,        -- observação/motivo do admin
    reviewed_by     uuid REFERENCES users(id),
    reviewed_at     timestamptz,
    paid_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT NOW(),
    updated_at      timestamptz NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_payout_requests_org    ON payout_requests(organization_id);
CREATE INDEX IF NOT EXISTS idx_payout_requests_status ON payout_requests(status);

COMMIT;
