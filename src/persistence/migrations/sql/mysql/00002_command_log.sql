-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE api_command_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL) ENGINE=InnoDB;
CREATE TABLE api_commands (id VARBINARY(64) PRIMARY KEY, actor_id VARBINARY(64) NOT NULL, connection_id VARBINARY(64) NOT NULL, operation VARBINARY(256) NOT NULL, key_hash VARBINARY(64) NOT NULL, expires_us BIGINT NOT NULL, payload LONGTEXT NOT NULL, UNIQUE KEY api_commands_key (actor_id,connection_id,operation,key_hash), KEY api_commands_actor (actor_id), KEY api_commands_expiry (expires_us)) ENGINE=InnoDB;
INSERT INTO api_command_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_commands;
DROP TABLE api_command_lock;
