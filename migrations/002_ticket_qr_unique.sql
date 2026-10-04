-- ============================================================================
-- 002_ticket_qr_unique.sql
-- Índice único em tickets.qr_code.
--
-- O token do QR code é a identidade do ingresso na portaria: o PATCH de
-- check-in agora localiza o ingresso por ele
-- (PATCH /org/:slug/events/:id/checkin-data com body {qr_code}).
--
-- Sem unicidade, dois ingressos poderiam compartilhar o mesmo token, e o
-- check-in marcaria um ingresso diferente do que a pessoa realmente mostrou —
-- o pior tipo de bug debilitar um ingresso, porque ninguém percebe até a
-- fila travar. A unicidade é a garantia de que "o QR que ela mostrou é o
-- ingresso dela".
--
-- A geração (orderservice.GenerateQRCode) já usa crypto/rand com 8 bytes
-- (~1.8e14 de combinações), então colisão na prática é irrelevante; o índice
-- é a rede de segurança, não a fonte da aleatoriedade.
--
-- Idempotente: pode rodar mais de uma vez sem efeito colateral.
--
-- ANTES de rodar, confira se o índice já não existe:
--     SELECT indexname FROM pg_indexes
--      WHERE tablename = 'tickets' AND indexname = 'tickets_qr_code_key';
--
-- Se já existir uma constraint UNIQUE sobre qr_code com outro nome, esta
-- migration falha com "relation already exists" — nesse caso comente o
-- bloco abaixo. Se existirem ingressos duplicados na base, o índice falha com
-- "could not create unique constraint"; corrija antes
-- (SELECT qr_code, COUNT(*) FROM tickets GROUP BY qr_code HAVING COUNT(*) > 1).
-- ============================================================================

BEGIN;

CREATE UNIQUE INDEX IF NOT EXISTS tickets_qr_code_key
    ON tickets (qr_code);

COMMIT;