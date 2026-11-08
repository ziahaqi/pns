-- One active FCM token per device, enforced by the database.
-- Concurrent registrations could leave a device with several active tokens,
-- so first keep only the newest active token per device, then add a partial
-- unique index. Both statements are safe to run on every start.

update device_tokens d
set status = 'inactive'
where d.status = 'active'
  and exists (
    select 1
    from device_tokens n
    where n.device_id = d.device_id
      and n.status = 'active'
      and (n.created_at, n.id) > (d.created_at, d.id)
  );

create unique index if not exists idx_device_tokens_one_active
    on device_tokens (device_id)
    where status = 'active';
