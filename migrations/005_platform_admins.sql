-- ============================================================================
-- 005_platform_admins.sql
-- Admins da PLATAFORMA (dono do Reppy), atrelados a public.users.
--
-- Diferente de organization_members.role ('admin' dentro de uma org), esta
-- tabela dá acesso ao painel /admin (ver taxas, definir taxa por org, ver
-- escrow e pagar pedidos de repasse).
--
-- Bootstrap do primeiro admin (rodar separadamente, troque o e-mail):
--   INSERT INTO platform_admins (user_id)
--   SELECT id FROM users WHERE lower(email) = lower('seu-email@exemplo.com')
--   ON CONFLICT (user_id) DO NOTHING;
--
-- Idempotente.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS platform_admins (
    user_id    uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    role       text NOT NULL DEFAULT 'admin',   -- 'admin' | 'owner'
    created_at timestamptz NOT NULL DEFAULT NOW()
);

COMMIT;
