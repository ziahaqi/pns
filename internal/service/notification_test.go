package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPayloadIsValidJSONWhateverTheText(t *testing.T) {
	msg := PushMessage{
		Token: "tok-1",
		Title: `Diskon "Akhir Tahun"`,
		Body:  "Line one\nLine two with a back\\slash and a tab\t",
	}
	body, err := payload(msg)
	if err != nil {
		t.Fatal(err)
	}
	var got fcmRequest
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("payload is not valid JSON: %v\n%s", err, body)
	}
	if got.To != msg.Token || got.Notification.Title != msg.Title || got.Notification.Body != msg.Body {
		t.Fatalf("round trip changed the message: %+v", got)
	}
}

func TestSendUsesTheServerKeyHeader(t *testing.T) {
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		io.Copy(io.Discard, r.Body)
	}))
	defer srv.Close()

	n := NewNotificationService("fcm-key")
	n.APIURL = srv.URL
	if err := n.Send(context.Background(), PushMessage{Token: "t", Title: "x", Body: "y"}); err != nil {
		t.Fatal(err)
	}
	if auth != "key=fcm-key" {
		t.Fatalf("Authorization = %q, want %q", auth, "key=fcm-key")
	}
}

func TestSendReportsFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "InvalidRegistration", http.StatusBadRequest)
	}))
	n := NewNotificationService("fcm-key")
	n.APIURL = srv.URL
	if err := n.Send(context.Background(), PushMessage{Token: "t"}); err == nil {
		t.Fatal("a 400 from FCM: want an error, not silent success")
	}

	srv.Close() // now nothing is listening
	if err := n.Send(context.Background(), PushMessage{Token: "t"}); err == nil {
		t.Fatal("an unreachable endpoint: want an error, not silent success")
	}
}

func TestConfigured(t *testing.T) {
	if NewNotificationService("").Configured() {
		t.Fatal("no key: want not configured")
	}
	if !NewNotificationService("k").Configured() {
		t.Fatal("with a key: want configured")
	}
}
