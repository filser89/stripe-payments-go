-- +goose Up
CREATE TABLE should_rollback (id integer PRIMARY KEY);
SELECT missing_function();
-- +goose Down
DROP TABLE should_rollback;
