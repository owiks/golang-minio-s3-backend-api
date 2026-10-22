-- +goose Up
CREATE TABLE api_operation_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL);
CREATE TABLE api_operations (id TEXT COLLATE BINARY PRIMARY KEY, actor_id TEXT COLLATE BINARY NOT NULL, connection_id TEXT COLLATE BINARY NOT NULL, state TEXT NOT NULL, revision BIGINT NOT NULL, created_us BIGINT NOT NULL, expires_us BIGINT NOT NULL, lease_us BIGINT NOT NULL, last_served_us BIGINT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX api_operations_scope ON api_operations(actor_id,connection_id);
CREATE INDEX api_operations_queue ON api_operations(state,lease_us,created_us);
CREATE INDEX api_operations_expiry ON api_operations(expires_us);
INSERT INTO api_operation_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_operations;
DROP TABLE api_operation_lock;
