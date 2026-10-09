-- +goose Up
CREATE TABLE foundation_fixture (id integer PRIMARY KEY, value text NOT NULL);
-- +goose Down
DROP TABLE foundation_fixture;
