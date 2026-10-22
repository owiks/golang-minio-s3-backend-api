package database

import (
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

func connect(c Config) (*sql.DB, error) {
	// Validate embedded callers too. No user-provided connection string reaches a
	// driver parser, so passwords and database names remain literal field values.
	d, err := c.Database.Validated()
	if err != nil || d.Driver == "memory" {
		return nil, ErrOpen
	}
	if d.Driver == "sqlite" {
		path, err := filepath.Abs(d.SQLitePath)
		if err != nil {
			return nil, ErrOpen
		}
		query := url.Values{"_pragma": {"foreign_keys(1)", "busy_timeout(5000)"}}
		u := url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: query.Encode()}
		return sql.Open("sqlite", u.String())
	}
	secure, err := databaseTLS(d)
	if err != nil {
		return nil, ErrOpen
	}
	if d.Driver == "mysql" {
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.DBName = d.User, d.Password, d.Name
		cfg.ParseTime = true // Goose version history and MySQL lock leases use DATETIME.
		cfg.Net, cfg.Addr = "tcp", net.JoinHostPort(d.Host, strconv.Itoa(d.Port))
		cfg.Timeout, cfg.TLS = 10*time.Second, secure
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, ErrOpen
		}
		return sql.OpenDB(connector), nil
	}
	cfg, err := postgresConfig(d, secure)
	if err != nil {
		return nil, ErrOpen
	}
	return stdlib.OpenDB(*cfg), nil
}

func postgresConfig(d config.Database, secure *tls.Config) (*pgx.ConnConfig, error) {
	// pgx requires ParseConfig to initialise its internal defaults. All connection
	// fields are then assigned explicitly; environment fallback targets are removed.
	cfg, err := pgx.ParseConfig("host=localhost port=5432 user=api dbname=api sslmode=disable")
	if err != nil {
		return nil, ErrOpen
	}
	cfg.Host, cfg.Port, cfg.User, cfg.Password, cfg.Database = d.Host, uint16(d.Port), d.User, d.Password, d.Name
	cfg.TLSConfig, cfg.Fallbacks = secure, nil
	cfg.ConnectTimeout = 10 * time.Second
	cfg.RuntimeParams = map[string]string{}
	return cfg, nil
}

func databaseTLS(d config.Database) (*tls.Config, error) {
	if d.TLSMode == "disable" {
		return nil, nil
	}
	cfg := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: d.Host}
	if d.TLSCAFile == "" {
		return cfg, nil
	}
	f, err := os.Open(d.TLSCAFile)
	if err != nil {
		return nil, ErrOpen
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 1024*1024+1))
	if err != nil || len(raw) > 1024*1024 {
		return nil, ErrOpen
	}
	cfg.RootCAs = x509.NewCertPool()
	if !cfg.RootCAs.AppendCertsFromPEM(raw) {
		return nil, ErrOpen
	}
	return cfg, nil
}
