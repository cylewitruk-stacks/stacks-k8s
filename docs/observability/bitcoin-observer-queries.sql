-- Requires the Bitcoin observer with both NetworkTelemetry metrics and logs enabled.
-- Replace <prefix>, <network-uid>, <telemetry-uid>, <from> and <to>; use bounded UTC windows.

-- Bitcoin heights and collection availability: compare samples only while observer_success is 1.
SELECT greptime_timestamp, participant_uid, pod_uid, greptime_value AS reported_height
FROM bitcoin_block_height
WHERE network_uid = '<network-uid>' AND telemetry_uid = '<telemetry-uid>'
  AND source = 'actor-native'
  AND greptime_timestamp >= '<from>' AND greptime_timestamp < '<to>'
ORDER BY greptime_timestamp LIMIT 500;

SELECT greptime_timestamp, participant_uid, pod_uid, greptime_value AS collection_succeeded
FROM bitcoin_observer_success
WHERE network_uid = '<network-uid>' AND telemetry_uid = '<telemetry-uid>'
  AND source = 'actor-native'
  AND greptime_timestamp >= '<from>' AND greptime_timestamp < '<to>'
ORDER BY greptime_timestamp LIMIT 500;

-- Exact tip hashes/chainwork and branches live in JSON log bodies, never metric labels.
-- Inspect success and the startedAt/observedAt window; equal heights need not share a chain.
SELECT timestamp, participant_uid, pod_uid, body
FROM <prefix>_logs
WHERE network_uid = '<network-uid>'
  AND source = 'container-log' AND body LIKE '%BitcoinObservation%'
  AND timestamp >= '<from>' AND timestamp < '<to>'
ORDER BY timestamp LIMIT 200;
