package repository

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/pns/internal/db"
)

// testPool connects to the database named by PNS_TEST_DATABASE_URL and
// migrates it, or skips the test when the variable is not set.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("PNS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PNS_TEST_DATABASE_URL to run the database tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestUpsertActiveLeavesOneActiveTokenUnderConcurrency(t *testing.T) {
	repo := NewDeviceRepository(testPool(t))
	ctx := context.Background()
	prefix := fmt.Sprintf("race-%d", time.Now().UnixNano())

	for i := 0; i < 20; i++ {
		device := fmt.Sprintf("%s-%d", prefix, i)
		var wg sync.WaitGroup
		errs := make(chan error, 4)
		for j := 0; j < 4; j++ {
			wg.Add(1)
			go func(j int) {
				defer wg.Done()
				if _, err := repo.UpsertActive(ctx, device, fmt.Sprintf("%s-tok-%d", device, j)); err != nil {
					errs <- err
				}
			}(j)
		}
		wg.Wait()
		close(errs)
		for err := range errs {
			t.Fatalf("concurrent registration failed: %v", err)
		}
		n, err := repo.CountActiveByDevice(ctx, device)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Fatalf("device %s has %d active tokens after concurrent registrations, want 1", device, n)
		}
	}
}

func TestReRegisteringAnOldTokenMakesItTheActiveOne(t *testing.T) {
	repo := NewDeviceRepository(testPool(t))
	ctx := context.Background()
	device := fmt.Sprintf("rereg-%d", time.Now().UnixNano())

	for _, token := range []string{"a", "b", "a"} {
		if _, err := repo.UpsertActive(ctx, device, token); err != nil {
			t.Fatal(err)
		}
	}
	tokens, err := repo.TokensByDevice(ctx, device)
	if err != nil {
		t.Fatal(err)
	}
	status := map[string]string{}
	for _, tok := range tokens {
		status[tok.FCMToken] = tok.Status
	}
	if status["a"] != "active" || status["b"] != "inactive" || len(status) != 2 {
		t.Fatalf("got %v, want a active and b inactive", status)
	}
}
