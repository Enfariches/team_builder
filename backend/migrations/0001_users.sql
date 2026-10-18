-- +goose Up
CREATE TABLE users (
    id            BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    login         VARCHAR(64)  NOT NULL UNIQUE,
    password_hash TEXT         NOT NULL,
    first_name    VARCHAR(255) NOT NULL,
    middle_name   VARCHAR(255),
    last_name     VARCHAR(255) NOT NULL,
    link          VARCHAR(255),
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ  NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE users;
