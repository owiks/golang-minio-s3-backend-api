package database

import (
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
)

func TestPostgresConnectionFieldsRemainLiteral(t *testing.T) {
	t.Setenv("PGOPTIONS", "--search_path=untrusted")
	d := config.Database{Driver: "postgres", Host: "::1", Port: 5432, User: "user:/@'", Password: "  password:/@?#%'  ", Name: "database ?#%'", TLSMode: "verify-full"}
	secure, err := databaseTLS(d)
	if err != nil || secure.InsecureSkipVerify || secure.ServerName != d.Host || secure.MinVersion < tls.VersionTLS12 {
		t.Fatal("TLS verification is not enforced", err)
	}
	c, err := postgresConfig(d, secure)
	if err != nil || c.Host != d.Host || c.Port != uint16(d.Port) || c.User != d.User || c.Password != d.Password || c.Database != d.Name || len(c.Fallbacks) != 0 || len(c.RuntimeParams) != 0 {
		t.Fatal("connection fields changed or environment fallback retained", err)
	}
	if _, err = databaseTLS(config.Database{TLSMode: "verify-full", TLSCAFile: filepath.Join(t.TempDir(), "missing.pem")}); err == nil {
		t.Fatal("missing CA accepted")
	}
}

func TestSQLiteUsesLiteralFilePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state ?#%'+.db")
	s, err := Open(context.Background(), Config{Database: config.Database{Driver: "sqlite", SQLitePath: path}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatal("SQLite path was interpreted as URL options", err)
	}
}
