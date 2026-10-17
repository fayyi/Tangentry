-- Every saved source receives the product default when no policy has ever
-- been stored. RSS and YouTube use conditional checks; Bilibili uses its
-- regular snapshot fetcher.
INSERT OR IGNORE INTO subscription_refresh(id, payload)
SELECT id,
       json_patch(json_object(
         'state', json_object(
           'enabled', 0,
           'intervalSeconds', 7200,
           'lastAttemptAt', 0,
           'lastCheckedAt', 0,
           'nextCheckAt', created_at + 7200,
           'failures', 0,
           'contentRevision', 0
         ),
         'validators', json_object('eTag', '', 'lastModified', '', 'resourceURL', '')
       ), '{"state":{"enabled":true}}')
FROM subscriptions;

-- Empty records were created by older builds without a persisted policy.
-- Keep any explicit interval (including a user-selected one hour) intact.
UPDATE subscription_refresh
SET payload = json_set(
  payload,
  '$.state.intervalSeconds', 7200,
  '$.state.enabled', json('true')
)
WHERE json_extract(payload, '$.state.intervalSeconds') IS NULL
   OR json_extract(payload, '$.state.intervalSeconds') = 0;
