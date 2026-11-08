package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// NotificationService is a thin wrapper around the FCM HTTP endpoint.
// Replace APIURL/ServerKey with your own values for production.
//
// Note: this speaks the legacy FCM HTTP protocol (fcm/send with a server key),
// which Google has shut down; moving to the HTTP v1 API needs a service
// account and an OAuth access token.
type NotificationService struct {
	APIURL    string
	ServerKey string
	Client    *http.Client
}

type PushMessage struct {
	Token string
	Title string
	Body  string
}

func NewNotificationService(serverKey string) *NotificationService {
	return &NotificationService{
		APIURL:    "https://fcm.googleapis.com/fcm/send",
		ServerKey: serverKey,
		Client:    &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured reports whether there is a key to send with.
func (s *NotificationService) Configured() bool {
	return s.ServerKey != ""
}

type fcmNotification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

type fcmRequest struct {
	To           string          `json:"to"`
	Notification fcmNotification `json:"notification"`
}

// payload encodes a message as JSON. Encoding it properly matters: titles and
// descriptions come from the admin form and may contain quotes, backslashes
// and line breaks.
func payload(msg PushMessage) ([]byte, error) {
	return json.Marshal(fcmRequest{To: msg.Token, Notification: fcmNotification{Title: msg.Title, Body: msg.Body}})
}

// Send delivers one message and returns an error when it was not accepted,
// so callers can count and log failures instead of assuming success.
func (s *NotificationService) Send(ctx context.Context, msg PushMessage) error {
	body, err := payload(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.APIURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "key="+s.ServerKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return fmt.Errorf("send push: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("send push: FCM returned %d: %s", resp.StatusCode, bytes.TrimSpace(snippet))
	}
	return nil
}
