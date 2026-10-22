-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE api_write_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL) ENGINE=InnoDB;
CREATE TABLE api_connections (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, payload LONGTEXT NOT NULL, KEY api_connections_owner (owner)) ENGINE=InnoDB;
CREATE TABLE api_accesses (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, connection_id VARBINARY(64) NOT NULL, payload LONGTEXT NOT NULL, KEY api_accesses_owner (owner, connection_id), FOREIGN KEY (connection_id) REFERENCES api_connections(id)) ENGINE=InnoDB;
CREATE TABLE api_events (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, at_us BIGINT NOT NULL, payload LONGTEXT NOT NULL, KEY api_events_owner (owner,at_us)) ENGINE=InnoDB;
INSERT INTO api_write_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_accesses;
DROP TABLE api_connections;
DROP TABLE api_events;
DROP TABLE api_write_lock;
DROP TABLE IF EXISTS api_schema;
