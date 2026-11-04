-- +goose Up
CREATE TABLE forms (
    id               BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id          BIGINT       NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    name             VARCHAR(255) NOT NULL,
    experience       SMALLINT     NOT NULL DEFAULT 0 CHECK (experience BETWEEN 0 AND 10),
    skills           TEXT[]       NOT NULL DEFAULT '{}',
    role_preferences JSONB        NOT NULL DEFAULT '[]',
    comment          TEXT,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- Уникальности по user_id нет намеренно: анкет у пользователя может быть несколько.
CREATE INDEX forms_user_id_idx ON forms (user_id);

-- Внешний ключ members.form_id на forms(id) должен быть ON DELETE SET NULL:
-- удаление анкеты не ломает участие, снимок остаётся в members.form_snapshot.

-- +goose Down
DROP TABLE forms;
