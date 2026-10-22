CREATE TABLE IF NOT EXISTS api_schema (version INTEGER PRIMARY KEY, checksum VARCHAR(64) NOT NULL);
CREATE TABLE IF NOT EXISTS api_write_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL);
CREATE TABLE IF NOT EXISTS api_connections (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS api_accesses (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, connection_id VARCHAR(64) COLLATE "C" NOT NULL, payload TEXT NOT NULL, FOREIGN KEY (connection_id) REFERENCES api_connections(id));
CREATE TABLE IF NOT EXISTS api_events (id VARCHAR(64) COLLATE "C" PRIMARY KEY, owner VARCHAR(64) COLLATE "C" NOT NULL, at_us BIGINT NOT NULL, payload TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS api_connections_owner ON api_connections(owner);
CREATE INDEX IF NOT EXISTS api_accesses_owner ON api_accesses(owner,connection_id);
CREATE INDEX IF NOT EXISTS api_events_owner ON api_events(owner,at_us);
