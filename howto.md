# Quick How-To

1) Start services
- With Docker: `docker compose up --build`
- Without Docker: ensure PostgreSQL is running and env vars are set, then `go run ./cmd/server`

2) Login to admin
- Open `http://localhost:8080/login`
- Use `ADMIN_EMAIL` / `ADMIN_PASSWORD` (auto-created on first start)

3) Register a device token (from mobile client or curl)
```bash
curl -X POST http://localhost:8080/api/v1/devices/register \
  -H "Content-Type: application/json" \
  -H "X-Server-Key: $SERVER_KEY" \
  -d '{"device_id":"abc123","fcm_device_token":"token-1"}'
```
- Only one token per `device_id` will remain `active`; older ones are set to `inactive`.

4) Manage promotions
- Create/edit via the admin UI.
- Click **Activate & send** to mark active and trigger push to all active tokens.

5) Manage users
- Use the Users section in the admin UI to add or remove admins.

Environment variables (must be set)
- `SERVER_KEY` – shared secret for the device API (it ships in the mobile app)
- `SESSION_SECRET` – cookie signing key; required, no default
- `FCM_SERVER_KEY` – optional key for sending pushes; when unset, activating a promotion sends nothing
- `DATABASE_URL` – PostgreSQL DSN
- `ADMIN_EMAIL`/`ADMIN_PASSWORD` – optional bootstrap admin

