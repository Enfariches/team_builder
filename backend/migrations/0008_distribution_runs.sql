-- +goose Up
-- roster — отсортированные id участников на момент прогона: по нему publish
-- понимает, что состав изменился после preview. result и metrics — jsonb,
-- потому что прогон неизменяем и читается целиком. team_ids заполняется при
-- публикации в порядке result.teams: так повторный publish отдаёт те же id.
CREATE TABLE distribution_runs (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    split_id     BIGINT      NOT NULL REFERENCES splits (id) ON DELETE CASCADE,
    algorithm    VARCHAR(32) NOT NULL,
    seed         BIGINT      NOT NULL,
    roster       BIGINT[]    NOT NULL,
    result       JSONB       NOT NULL,
    metrics      JSONB       NOT NULL,
    team_ids     BIGINT[],
    published_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX distribution_runs_split_id_idx ON distribution_runs (split_id, id DESC);

-- Страховка идемпотентности публикации на уровне БД: второй опубликованный
-- прогон одного пространства невозможен, даже если код ошибётся.
CREATE UNIQUE INDEX distribution_runs_published_uidx
    ON distribution_runs (split_id) WHERE published_at IS NOT NULL;

-- +goose Down
DROP TABLE distribution_runs;
