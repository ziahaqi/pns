package db

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestMigrateKeepsTheNewestActiveTokenAndIsRepeatable(t *testing.T) {
	dsn := os.Getenv("PNS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PNS_TEST_DATABASE_URL to run the database tests")
	}
	ctx := context.Background()
	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	// A database from before the index, where a race left three active tokens.
	device := fmt.Sprintf("dupe-%d", time.Now().UnixNano())
	if _, err := pool.Exec(ctx, `drop index if exists idx_device_tokens_one_active`); err != nil {
		t.Fatal(err)
	}
	for i, age := range []string{"3 hours", "2 hours", "1 hour"} {
		if _, err := pool.Exec(ctx,
			`insert into device_tokens (device_id, fcm_device_token, status, created_at)
			 values ($1, $2, 'active', now() - $3::interval)`,
			device, fmt.Sprintf("tok-%d", i), age); err != nil {
			t.Fatal(err)
		}
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate over duplicates: %v", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var active string
	var n int
	if err := pool.QueryRow(ctx,
		`select count(*), min(fcm_device_token) from device_tokens where device_id = $1 and status = 'active'`,
		device).Scan(&n, &active); err != nil {
		t.Fatal(err)
	}
	if n != 1 || active != "tok-2" {
		t.Fatalf("got %d active (%q), want only the newest, tok-2", n, active)
	}

	if _, err := pool.Exec(ctx,
		`insert into device_tokens (device_id, fcm_device_token, status) values ($1, 'tok-extra', 'active')`,
		device); err == nil {
		t.Fatal("a second active token for the device was accepted; the unique index is missing")
	}
}
