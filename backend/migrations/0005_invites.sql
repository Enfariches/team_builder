-- +goose Up
-- ON DELETE CASCADE: удаление пространства убирает его инвайты, при этом модулю
-- split не нужно обращаться к чужой таблице.
CREATE TABLE invites (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    split_id    BIGINT      NOT NULL REFERENCES splits (id) ON DELETE CASCADE,
    token       VARCHAR(64) NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ,
    max_uses    INT         CHECK (max_uses >= 1),
    uses_count  INT         NOT NULL DEFAULT 0 CHECK (uses_count >= 0),
    revoked_at  TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX invites_split_id_idx ON invites (split_id);

-- +goose Down
DROP TABLE invites;
