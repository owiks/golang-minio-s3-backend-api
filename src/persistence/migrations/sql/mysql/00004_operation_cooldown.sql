-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE api_operations ADD COLUMN not_before_us BIGINT NOT NULL DEFAULT 0;
CREATE INDEX api_operations_cooldown ON api_operations(connection_id,not_before_us);

-- +goose Down
DROP INDEX api_operations_cooldown ON api_operations;
ALTER TABLE api_operations DROP COLUMN not_before_us;
