-- +goose Up
-- ON DELETE CASCADE от splits: удаление пространства убирает участников без
-- участия split. form_id — SET NULL: удаление анкеты не ломает участие,
-- замороженный снимок остаётся в form_snapshot.
CREATE TABLE members (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    split_id      BIGINT       NOT NULL REFERENCES splits (id) ON DELETE CASCADE,
    user_id       BIGINT       NOT NULL REFERENCES users (id),
    form_id       BIGINT       REFERENCES forms (id) ON DELETE SET NULL,
    form_snapshot JSONB        NOT NULL,
    -- на момент вступления команд нет; FK на teams добавит миграция модуля team
    team_id       BIGINT,
    assigned_role VARCHAR(255),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    UNIQUE (split_id, user_id)
);

CREATE INDEX members_user_id_idx ON members (user_id);

-- +goose Down
DROP TABLE members;
