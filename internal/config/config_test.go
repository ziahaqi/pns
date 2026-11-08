package config

import "testing"

func TestLoadRequiresSecrets(t *testing.T) {
	t.Setenv("SERVER_KEY", "device-key")
	t.Setenv("SESSION_SECRET", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load without SESSION_SECRET: want an error, a default secret would let anyone forge an admin session")
	}

	t.Setenv("SESSION_SECRET", "session-secret")
	t.Setenv("SERVER_KEY", "")
	if _, err := Load(); err == nil {
		t.Fatal("Load without SERVER_KEY: want an error")
	}
}

func TestLoadKeepsTheFCMKeyApartFromTheDeviceKey(t *testing.T) {
	t.Setenv("SERVER_KEY", "device-key")
	t.Setenv("SESSION_SECRET", "session-secret")
	t.Setenv("FCM_SERVER_KEY", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FCMServerKey != "" {
		t.Fatalf("FCMServerKey = %q, want empty: the device key must never be used to send pushes", cfg.FCMServerKey)
	}

	t.Setenv("FCM_SERVER_KEY", "fcm-key")
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FCMServerKey != "fcm-key" || cfg.ServerKey != "device-key" {
		t.Fatalf("got FCM %q, device %q", cfg.FCMServerKey, cfg.ServerKey)
	}
}
