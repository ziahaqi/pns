package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/example/pns/internal/config"
	"github.com/example/pns/internal/db"
	"github.com/example/pns/internal/model"
	"github.com/example/pns/internal/repository"
)

// Each slow send takes a second; six of them used to outlast the single
// five-second deadline the whole loop shared, and the rest were skipped.
func TestPushReachesEveryActiveToken(t *testing.T) {
	dsn := os.Getenv("PNS_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set PNS_TEST_DATABASE_URL to run the database tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}

	prefix := fmt.Sprintf("push-%d", time.Now().UnixNano())
	var mu sync.Mutex
	got := map[string]bool{}
	fcm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			To string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		if strings.HasPrefix(req.To, prefix) {
			time.Sleep(time.Second)
			mu.Lock()
			got[req.To] = true
			mu.Unlock()
		}
	}))
	defer fcm.Close()

	devices := repository.NewDeviceRepository(pool)
	promos := repository.NewPromotionRepository(pool)
	s := New(config.Config{ServerKey: "device-key", SessionSecret: "s", FCMServerKey: "fcm-key"}, ctx, struct {
		Users   *repository.UserRepository
		Devices *repository.DeviceRepository
		Promos  *repository.PromotionRepository
	}{repository.NewUserRepository(pool), devices, promos})
	s.notifier.APIURL = fcm.URL

	for i := 0; i < 6; i++ {
		if _, err := devices.UpsertActive(ctx, fmt.Sprintf("%s-device-%d", prefix, i), fmt.Sprintf("%s-token-%d", prefix, i)); err != nil {
			t.Fatal(err)
		}
	}
	promo, err := promos.Create(ctx, model.Promotion{
		Title: "Year-end sale", Description: "Line one\nLine two",
		StartDate: time.Now(), EndDate: time.Now(), Status: "active",
	})
	if err != nil {
		t.Fatal(err)
	}

	s.pushPromotion(promo.ID)

	if len(got) != 6 {
		t.Fatalf("delivered %d of 6 pushes", len(got))
	}
}
