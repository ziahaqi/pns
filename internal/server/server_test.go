package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/gin-contrib/sessions"
	"github.com/gin-gonic/gin"

	"github.com/example/pns/internal/config"
	"github.com/example/pns/internal/repository"
)

func init() { gin.SetMode(gin.TestMode) }

func newServer(cfg config.Config) *Server {
	return New(cfg, context.Background(), struct {
		Users   *repository.UserRepository
		Devices *repository.DeviceRepository
		Promos  *repository.PromotionRepository
	}{})
}

func TestServerKeyGuard(t *testing.T) {
	s := &Server{cfg: config.Config{ServerKey: "device-key"}}
	r := gin.New()
	r.POST("/x", s.serverKeyGuard(), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	cases := []struct {
		header, value string
		want          int
	}{
		{"", "", http.StatusUnauthorized},
		{"X-Server-Key", "wrong", http.StatusUnauthorized},
		{"X-Server-Key", "device-key-but-longer", http.StatusUnauthorized},
		{"X-Server-Key", "device-key", http.StatusNoContent},
		{"server_key", "device-key", http.StatusNoContent},
	}
	for _, tc := range cases {
		req := httptest.NewRequest(http.MethodPost, "/x", nil)
		if tc.header != "" {
			req.Header.Set(tc.header, tc.value)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != tc.want {
			t.Errorf("%s=%q: got %d, want %d", tc.header, tc.value, w.Code, tc.want)
		}
	}
}

func TestPromotionValidation(t *testing.T) {
	valid := url.Values{"title": {"Year-end sale"}, "start_date": {"2025-12-01"}, "end_date": {"2025-12-31"}, "status": {"created"}}
	with := func(key, value string) url.Values {
		v := url.Values{}
		for k, vs := range valid {
			v[k] = append([]string(nil), vs...)
		}
		v.Set(key, value)
		return v
	}
	parse := func(form url.Values) error {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(form.Encode()))
		c.Request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		_, err := (&Server{}).promotionFromRequest(c)
		return err
	}

	if err := parse(valid); err != nil {
		t.Fatalf("valid promotion rejected: %v", err)
	}
	if err := parse(with("status", "")); err != nil {
		t.Fatalf("missing status should default to created: %v", err)
	}
	for name, form := range map[string]url.Values{
		"blank title":      with("title", "   "),
		"bad start date":   with("start_date", "01/12/2025"),
		"end before start": with("end_date", "2025-11-30"),
		"unknown status":   with("status", "deleted"),
	} {
		if err := parse(form); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}

func TestSessionCookieIsSameSiteLax(t *testing.T) {
	s := newServer(config.Config{ServerKey: "k", SessionSecret: "session-secret"})
	// what handleLogin does after a good password, without needing a database
	s.router.GET("/test-session", func(c *gin.Context) {
		session := sessions.Default(c)
		session.Set("user_id", "u-1")
		_ = session.Save()
	})
	w := httptest.NewRecorder()
	s.router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test-session", nil))
	cookie := w.Header().Get("Set-Cookie")
	if !strings.Contains(cookie, "SameSite=Lax") || !strings.Contains(cookie, "HttpOnly") {
		t.Fatalf("Set-Cookie = %q, want HttpOnly and SameSite=Lax", cookie)
	}
}

func TestNoPushesWithoutAnFCMKey(t *testing.T) {
	var calls int32
	fcm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { atomic.AddInt32(&calls, 1) }))
	defer fcm.Close()

	s := newServer(config.Config{ServerKey: "device-key", SessionSecret: "s"})
	s.notifier.APIURL = fcm.URL
	s.pushPromotion("any") // returns before touching the database
	if n := atomic.LoadInt32(&calls); n != 0 {
		t.Fatalf("sent %d pushes with no FCM key; the device key must never be used for FCM", n)
	}
}
