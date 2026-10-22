// Package databasefixtures creates random namespaces only in disposable loopback databases.
package databasefixtures

import (
	"context"
	"database/sql"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"

	"github.com/owiks/golang-minio-s3-backend-api/src/config"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/authstate"
	"github.com/owiks/golang-minio-s3-backend-api/src/services/securetoken"
)

func Database(t *testing.T, driver string) config.State {
	t.Helper()
	if driver == "sqlite" {
		return config.State{Database: config.Database{Driver: driver, SQLitePath: filepath.Join(t.TempDir(), "state.db")}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	name := "auth_" + authstate.Digest(securetoken.New())[:20]
	password := "fixture:/@?#% password"
	database := config.Database{Driver: driver, Host: "127.0.0.1", Port: 15432, Name: name, User: "fixture", Password: password, TLSMode: "disable"}
	u := url.URL{Scheme: "postgres", Host: "127.0.0.1:15432", Path: "/api_state_test", User: url.UserPassword("fixture", password), RawQuery: "sslmode=disable"}
	var admin *sql.DB
	var err error
	if driver == "mysql" {
		database.Port, database.User = 13306, "root"
		cfg := mysql.NewConfig()
		cfg.User, cfg.Passwd, cfg.Net, cfg.Addr, cfg.DBName = "root", password, "tcp", "127.0.0.1:13306", "api_state_test"
		connector, e := mysql.NewConnector(cfg)
		if e != nil {
			t.Fatal(e)
		}
		admin = sql.OpenDB(connector)
	} else {
		admin, err = sql.Open("pgx", u.String())
	}
	if err != nil {
		t.Fatal(err)
	}
	create, drop := "CREATE DATABASE "+name, "DROP DATABASE "+name
	if driver == "mysql" {
		create += " CHARACTER SET utf8mb4"
	}
	if _, err = admin.ExecContext(ctx, create); err != nil {
		_ = admin.Close()
		t.Fatal("cannot create disposable fixture namespace", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.ExecContext(ctx, drop); err != nil {
			t.Error(err)
		}
		if driver == "mysql" {
			if _, err := admin.ExecContext(ctx, "DROP USER IF EXISTS '"+name+":user'@'%'"); err != nil {
				t.Error(err)
			}
		}
		_ = admin.Close()
	})
	if driver == "mysql" {
		// Exercise literal username separators, quotes and surrounding password
		// whitespace through the connector, without driver DSN escaping.
		database.User, database.Password = name+":user", "  fixture:/@?#% pass'word  "
		literal := strings.ReplaceAll(database.Password, "'", "''")
		if _, err := admin.ExecContext(ctx, "CREATE USER '"+database.User+"'@'%' IDENTIFIED BY '"+literal+"'"); err != nil {
			t.Fatal("cannot create disposable fixture user", err)
		}
		if _, err := admin.ExecContext(ctx, "GRANT ALL ON "+name+".* TO '"+database.User+"'@'%'"); err != nil {
			t.Fatal("cannot grant disposable fixture access", err)
		}
	}
	return config.State{Database: database}
}
