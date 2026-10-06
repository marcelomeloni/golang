-- ============================================================================
-- 003_organizations_platform_fee.sql
-- Colunas de taxa customizada por organização (nullable = herda a regra global).
--
-- Ficam como leitura no GET /org/:slug por enquanto, sem efeito no cálculo
-- (feehelper continua global). Servem de alavanca futura para promoções de
-- taxa para uma organização específica.
--
-- Idempotente: pode rodar mais de uma vez sem efeito colateral.
-- ============================================================================

BEGIN;

ALTER TABLE organizations
    ADD COLUMN IF NOT EXISTS platform_fee_percentage numeric(5, 2) DEFAULT NULL,
    ADD COLUMN IF NOT EXISTS platform_fee_fixed      numeric(10, 2) DEFAULT NULL;

COMMIT;