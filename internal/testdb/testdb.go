// Package testdb supplies disposable, explicitly opted-in PostGIS test schemas.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"github.com/jackc/pgx/v5"
	"os"
	"testing"
)

func Open(t *testing.T) (*pgx.Conn, func(context.Context) (*pgx.Conn, error)) {
	t.Helper()
	url := os.Getenv("B11K_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set B11K_TEST_DATABASE_URL to an isolated PostGIS database")
	}
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatal(err)
	}
	schema := "sync_test_" + hex.EncodeToString(b[:])
	ctx := context.Background()
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close(ctx)
		t.Fatal(err)
	}
	connect := func(ctx context.Context) (*pgx.Conn, error) {
		cfg, err := pgx.ParseConfig(url)
		if err != nil {
			return nil, err
		}
		cfg.RuntimeParams["search_path"] = schema + ",public"
		return pgx.ConnectConfig(ctx, cfg)
	}
	conn, err := connect(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close(ctx); admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); admin.Close(ctx) })
	return conn, connect
}
