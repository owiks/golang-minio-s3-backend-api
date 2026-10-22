-- +goose Up
CREATE TABLE api_command_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL);
CREATE TABLE api_commands (id TEXT COLLATE BINARY PRIMARY KEY, actor_id TEXT COLLATE BINARY NOT NULL, connection_id TEXT COLLATE BINARY NOT NULL, operation TEXT COLLATE BINARY NOT NULL, key_hash TEXT COLLATE BINARY NOT NULL, expires_us BIGINT NOT NULL, payload TEXT NOT NULL, UNIQUE(actor_id,connection_id,operation,key_hash));
CREATE INDEX api_commands_actor ON api_commands(actor_id);
CREATE INDEX api_commands_expiry ON api_commands(expires_us);
INSERT INTO api_command_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_commands;
DROP TABLE api_command_lock;
