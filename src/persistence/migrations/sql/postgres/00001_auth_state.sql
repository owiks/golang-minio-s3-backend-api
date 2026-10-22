-- +goose Up
CREATE TABLE api_write_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL);
CREATE TABLE api_connections (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, payload TEXT NOT NULL);
CREATE TABLE api_accesses (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, connection_id VARCHAR(64) COLLATE "C" NOT NULL, payload TEXT NOT NULL, FOREIGN KEY (connection_id) REFERENCES api_connections(id));
CREATE TABLE api_events (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, at_us BIGINT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX api_connections_owner ON api_connections(owner);
CREATE INDEX api_accesses_owner ON api_accesses(owner,connection_id);
CREATE INDEX api_events_owner ON api_events(owner,at_us);
INSERT INTO api_write_lock(id,revision) VALUES(1,0);

-- +goose Down
DROP TABLE api_accesses;
DROP TABLE api_connections;
DROP TABLE api_events;
DROP TABLE api_write_lock;
DROP TABLE IF EXISTS api_schema;
