-- +goose Up
-- ON DELETE CASCADE от splits: удаление пространства убирает команды без
-- участия split.
CREATE TABLE teams (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    split_id   BIGINT       NOT NULL REFERENCES splits (id) ON DELETE CASCADE,
    name       VARCHAR(255) NOT NULL,
    sort_order INT          NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX teams_split_id_idx ON teams (split_id);

-- Внешний ключ, обещанный в 0006_members.sql. SET NULL: удаление команды не
-- удаляет участника, он просто оказывается вне команд.
ALTER TABLE members
    ADD CONSTRAINT members_team_id_fkey FOREIGN KEY (team_id) REFERENCES teams (id) ON DELETE SET NULL;

-- +goose Down
ALTER TABLE members DROP CONSTRAINT members_team_id_fkey;
DROP TABLE teams;
