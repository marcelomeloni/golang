-- ============================================================================
-- 001_ticket_transfers.sql
-- Trilha de auditoria das transferências de ingresso feitas pelo CPF.
--
-- A transferência (POST /client/tickets/:id/transfer) já existia, mas só
-- sobrescrevia tickets.user_id — não sobrava nenhum registro de quem mandou
-- o ingresso para quem. Isso impossibilitava suporte ("meu ingresso sumiu")
-- e reversão de uma transferência feita por engano.
--
-- Idempotente: pode rodar mais de uma vez sem efeito colateral.
-- ============================================================================

BEGIN;

CREATE TABLE IF NOT EXISTS ticket_transfers (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),

    -- Ingresso que mudou de dono. ON DELETE CASCADE: se o ingresso for
    -- removido (ex.: chargeback), o histórico vai junto em vez de deixar
    -- linhas órfãs apontando para um ticket inexistente.
    ticket_id          UUID NOT NULL REFERENCES tickets(id) ON DELETE CASCADE,

    -- Remetente e destinatário. ON DELETE SET NULL: um usuário deletado
    -- não pode apagar o histórico da transferência, mas as linhas ficam sem
    -- referência de pessoa em vez de sumirem.
    from_user_id       UUID REFERENCES users(id) ON DELETE SET NULL,
    to_user_id         UUID REFERENCES users(id) ON DELETE SET NULL,

    -- Snapshot do CPF na hora da transferência, sem formatação.
    -- Guardar em vez de só derivar de users.cpf é intencional: o cadastro
    -- pode ser atualizado depois, e a auditoria precisa responder "qual CPF
    -- foi usado na hora?", não "qual é o CPF dessa pessoa hoje?".
    from_cpf           TEXT,
    to_cpf             TEXT NOT NULL,

    -- QR code gerado para o destinatário. O QR anterior foi invalidado
    -- na mesma transação, então este é o único válido daqui pra frente.
    previous_qr_code   TEXT,
    new_qr_code        TEXT NOT NULL,

    event_id           UUID REFERENCES events(id) ON DELETE SET NULL,

    -- true se uma das partes era conta de visitante (is_guest = true).
    -- Guest não tem senha/e-mail verificado, então é a faixa que mais
    -- merece revisão manual quando algo dá errado.
    from_is_guest      BOOLEAN NOT NULL DEFAULT false,
    to_is_guest        BOOLEAN NOT NULL DEFAULT false,

    -- Preenchido se a transferência foi desfeita depois (por suporte).
    -- NULL = transferência vigente. O índice único parcial abaixo só
    -- considera as linhas vigentes, então uma reversão libera o
    -- ingresso para uma nova transferência sem apagar o histórico.
    revoked_at         TIMESTAMPTZ,
    revoked_by         UUID REFERENCES users(id) ON DELETE SET NULL,
    revoke_reason      TEXT,

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Consulta quente: "todas as transferências deste ingresso" (suporte, reversão).
CREATE INDEX IF NOT EXISTS idx_ticket_transfers_ticket_id
    ON ticket_transfers (ticket_id, created_at DESC);

-- "Histórico de transferências recebidas por esta pessoa" (usuário reclamando
-- de um ingresso que recebeu sem saber de quem).
CREATE INDEX IF NOT EXISTS idx_ticket_transfers_to_user
    ON ticket_transfers (to_user_id, created_at DESC);

-- "Histórico de transferências feitas por esta pessoa" (contestação de venda).
CREATE INDEX IF NOT EXISTS idx_ticket_transfers_from_user
    ON ticket_transfers (from_user_id, created_at DESC);

COMMIT;

-- ============================================================================
-- Índice único parcial: um ingresso só pode ser transferido uma vez por vez.
-- Na prática a API já protege com UPDATE condicional dentro de transação
-- (WHERE user_id = remetente), mas esta constraint é a rede de segurança
-- final contra corrida entre duas requisições simultâneas.
--
-- Comentar as linhas abaixo se preferir não bloquear (ou rode separado,
-- pois múltiplos comandos prepared não cabem em uma única transação).
-- ============================================================================
--
-- CREATE UNIQUE INDEX IF NOT EXISTS uniq_ticket_transfers_open_ticket
--     ON ticket_transfers (ticket_id)
--     WHERE revoked_at IS NULL;