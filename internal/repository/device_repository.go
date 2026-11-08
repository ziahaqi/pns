package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/example/pns/internal/model"
)

type DeviceRepository struct {
	pool *pgxpool.Pool
}

func NewDeviceRepository(pool *pgxpool.Pool) *DeviceRepository {
	return &DeviceRepository{pool: pool}
}

// UpsertActive inserts or reactivates a token and marks all other tokens
// for the same device_id as inactive.
func (r *DeviceRepository) UpsertActive(ctx context.Context, deviceID, token string) (model.DeviceToken, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return model.DeviceToken{}, err
	}
	defer tx.Rollback(ctx)

	// Serialize registrations for the same device: without this, two concurrent
	// requests each deactivate the other's not-yet-committed token and both
	// insert theirs as active. The lock is released when the transaction ends.
	if _, err = tx.Exec(ctx, `select pg_advisory_xact_lock(hashtextextended($1, 0))`, deviceID); err != nil {
		return model.DeviceToken{}, err
	}

	_, err = tx.Exec(ctx, `update device_tokens set status='inactive' where device_id=$1 and fcm_device_token <> $2`, deviceID, token)
	if err != nil {
		return model.DeviceToken{}, err
	}

	var dt model.DeviceToken
	sql := `insert into device_tokens (device_id, fcm_device_token, status) 
	        values ($1, $2, 'active')
	        on conflict (device_id, fcm_device_token) do update set status='active'
	        returning id, device_id, fcm_device_token, status, created_at`
	if err := tx.QueryRow(ctx, sql, deviceID, token).Scan(&dt.ID, &dt.DeviceID, &dt.FCMToken, &dt.Status, &dt.CreatedAt); err != nil {
		return model.DeviceToken{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return model.DeviceToken{}, err
	}
	return dt, nil
}

func (r *DeviceRepository) ListActiveTokens(ctx context.Context) ([]model.DeviceToken, error) {
	rows, err := r.pool.Query(ctx, `select id, device_id, fcm_device_token, status, created_at from device_tokens where status='active' order by created_at desc`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []model.DeviceToken
	for rows.Next() {
		var dt model.DeviceToken
		if err := rows.Scan(&dt.ID, &dt.DeviceID, &dt.FCMToken, &dt.Status, &dt.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, dt)
	}
	return tokens, rows.Err()
}

func (r *DeviceRepository) TokensByDevice(ctx context.Context, deviceID string) ([]model.DeviceToken, error) {
	rows, err := r.pool.Query(ctx, `select id, device_id, fcm_device_token, status, created_at from device_tokens where device_id=$1 order by created_at desc`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tokens []model.DeviceToken
	for rows.Next() {
		var dt model.DeviceToken
		if err := rows.Scan(&dt.ID, &dt.DeviceID, &dt.FCMToken, &dt.Status, &dt.CreatedAt); err != nil {
			return nil, err
		}
		tokens = append(tokens, dt)
	}
	return tokens, rows.Err()
}

func (r *DeviceRepository) CountActiveByDevice(ctx context.Context, deviceID string) (int, error) {
	var count int
	err := r.pool.QueryRow(ctx, `select count(*) from device_tokens where device_id=$1 and status='active'`, deviceID).Scan(&count)
	return count, err
}

func (r *DeviceRepository) DeactivateToken(ctx context.Context, id int64) error {
	tag, err := r.pool.Exec(ctx, `update device_tokens set status='inactive' where id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("token %d not found", id)
	}
	return nil
}

