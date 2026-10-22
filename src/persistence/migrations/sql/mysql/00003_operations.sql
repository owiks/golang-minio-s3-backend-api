-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE api_operation_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL) ENGINE=InnoDB;
CREATE TABLE api_operations (id VARBINARY(64) PRIMARY KEY, actor_id VARBINARY(64) NOT NULL, connection_id VARBINARY(64) NOT NULL, state VARCHAR(16) NOT NULL, revision BIGINT NOT NULL, created_us BIGINT NOT NULL, expires_us BIGINT NOT NULL, lease_us BIGINT NOT NULL, last_served_us BIGINT NOT NULL, payload LONGTEXT NOT NULL, KEY api_operations_scope (actor_id,connection_id), KEY api_operations_queue (state,lease_us,created_us), KEY api_operations_expiry (expires_us)) ENGINE=InnoDB;
INSERT INTO api_operation_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_operations;
DROP TABLE api_operation_lock;
