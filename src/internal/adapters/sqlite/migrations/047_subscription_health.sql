-- Health fields are derived on read for compatibility with legacy JSON. Keep
-- the failure count indexed so the management center can order attention
-- items without decoding every refresh record in the application.
CREATE INDEX IF NOT EXISTS subscription_refresh_failures
ON subscription_refresh(CAST(json_extract(payload, '$.state.failures') AS INTEGER));
