CREATE TABLE IF NOT EXISTS api_schema (version INTEGER PRIMARY KEY, checksum VARCHAR(64) NOT NULL) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS api_write_lock (id INTEGER PRIMARY KEY, revision BIGINT NOT NULL) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS api_connections (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, payload LONGTEXT NOT NULL, KEY api_connections_owner (owner)) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS api_accesses (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, connection_id VARBINARY(64) NOT NULL, payload LONGTEXT NOT NULL, KEY api_accesses_owner (owner, connection_id), FOREIGN KEY (connection_id) REFERENCES api_connections(id)) ENGINE=InnoDB;
CREATE TABLE IF NOT EXISTS api_events (id VARBINARY(64) PRIMARY KEY, owner VARBINARY(64) NOT NULL, at_us BIGINT NOT NULL, payload LONGTEXT NOT NULL, KEY api_events_owner (owner,at_us)) ENGINE=InnoDB;
