-- ============================================================================
-- 008_remove_fixed_fee_default.sql
-- Remove o resíduo da taxa fixa de R$ 0,80 por ingresso.
--
-- O gateway atual não cobra custo fixo por transação. O percentual da
-- organização continua sendo a única regra de cálculo, com 10% até R$ 10.
--
-- Idempotente: pode rodar mais de uma vez sem efeito colateral.
-- ============================================================================

BEGIN;

ALTER TABLE organizations
    ALTER COLUMN platform_fee_fixed SET DEFAULT 0.00;

UPDATE organizations
   SET platform_fee_fixed = 0.00
 WHERE platform_fee_fixed = 0.80;

COMMIT;
