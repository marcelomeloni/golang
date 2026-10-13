-- ============================================================================
-- 004_organizations_fee_default.sql
-- Torna o percentual de taxa por organização a fonte de verdade da plataforma.
--
-- A regra global do feehelper (services/feehelper) continua como fallback, mas
-- agora o valor efetivo vem de organizations.platform_fee_percentage, gravado
-- como número percentual (8.00 = 8%).
--
-- Regras mantidas no código:
--   * ingressos > R$ 10  → platform_fee_percentage da org
--   * ingressos <= R$ 10 → 10% global (LowPricePercentage)
--   * piso mínimo        → R$ 0,80 global (MinimumFee)
--
-- Idempotente: pode rodar mais de uma vez sem efeito colateral.
-- ============================================================================

BEGIN;

-- Percentual padrão da plataforma: 8%  (armazenado como 8.00 = 8%)
ALTER TABLE organizations
    ALTER COLUMN platform_fee_percentage SET DEFAULT 8.00;

-- Backfill das organizações existentes (a coluna foi criada NULL na 003).
UPDATE organizations
   SET platform_fee_percentage = 8.00
 WHERE platform_fee_percentage IS NULL;

COMMIT;
