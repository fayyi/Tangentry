CREATE INDEX IF NOT EXISTS subscription_refresh_health
ON subscription_refresh(json_extract(payload, '$.state.status'));
