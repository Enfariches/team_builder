-- +goose Up
CREATE TABLE splits (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    owner_id         BIGINT       NOT NULL REFERENCES users (id),
    title            VARCHAR(255) NOT NULL,
    description      TEXT,
    status           VARCHAR(16)  NOT NULL DEFAULT 'Draft'
                       CHECK (status IN ('Draft', 'Open', 'Closed', 'Finished')),
    team_size        INT          NOT NULL CHECK (team_size >= 1),
    max_members      INT          CHECK (max_members >= 1),
    remainder_policy VARCHAR(32)  NOT NULL DEFAULT 'spread'
                       CHECK (remainder_policy IN ('smaller_last_team', 'spread', 'waitlist')),
    auto_finish_at   TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

CREATE INDEX splits_owner_id_idx ON splits (owner_id);

-- Внешние ключи на splits из members, invites и teams должны быть
-- ON DELETE CASCADE: так удаление пространства чистит их без участия split.
CREATE TABLE split_roles (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    split_id     BIGINT       NOT NULL REFERENCES splits (id) ON DELETE CASCADE,
    name         VARCHAR(255) NOT NULL,
    per_team_min INT          NOT NULL CHECK (per_team_min >= 0),
    per_team_max INT          NOT NULL CHECK (per_team_max >= 1),
    sort_order   INT          NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT now(),
    CHECK (per_team_min <= per_team_max),
    UNIQUE (split_id, name)
);

-- +goose Down
DROP TABLE split_roles;
DROP TABLE splits;
