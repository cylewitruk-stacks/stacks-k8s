-- Replace <prefix>, <network-uid>, <telemetry-uid>, <participant-uid>, <from> and <to>.
-- Time and identity predicates bound every scan. Timestamps are UTC.

-- Inventory/provenance and status changes, including deletion events.
SELECT timestamp, source, event_type, object_uid, participant_uid, body
FROM <prefix>_objects
WHERE network_uid = '<network-uid>'
  AND timestamp >= '<from>' AND timestamp < '<to>'
  AND source = 'stacksnetworkparticipants.network.stacks.org/v1alpha2'
ORDER BY timestamp LIMIT 200;

-- Native fault lifecycle and selected actor UIDs; inspect the redacted spec/status.
SELECT timestamp, event_type, object_uid, body
FROM <prefix>_objects
WHERE network_uid = '<network-uid>'
  AND timestamp >= '<from>' AND timestamp < '<to>'
  AND source = 'networkchaos.chaos-mesh.org/v1alpha1'
ORDER BY timestamp LIMIT 200;

-- Coverage failures are evidence, not assertions that the actor failed.
SELECT timestamp, source, body
FROM <prefix>_objects
WHERE network_uid = '<network-uid>'
  AND timestamp >= '<from>' AND timestamp < '<to>'
  AND event_type = 'CaptureGap'
ORDER BY timestamp LIMIT 200;

-- Summarize height extrema; these are not first/last values or a monotonic-progress proof.
SELECT participant_uid, pod_uid, min(greptime_value) AS minimum_height,
       max(greptime_value) AS greatest_height, count(*) AS samples
FROM stacks_node_stacks_tip_height
WHERE network_uid = '<network-uid>' AND telemetry_uid = '<telemetry-uid>'
  AND source = 'actor-native'
  AND greptime_timestamp >= '<from>' AND greptime_timestamp < '<to>'
GROUP BY participant_uid, pod_uid;

-- Verify scrape availability separately from protocol progress.
SELECT participant_uid, pod_uid, min(greptime_value) AS minimum_up,
       max(greptime_value) AS maximum_up, count(*) AS scrapes
FROM up
WHERE network_uid = '<network-uid>' AND telemetry_uid = '<telemetry-uid>'
  AND source = 'actor-native'
  AND greptime_timestamp >= '<from>' AND greptime_timestamp < '<to>'
GROUP BY participant_uid, pod_uid;

-- Inspect bounded actor logs around a selected event.
SELECT timestamp, participant_uid, pod_uid, body
FROM <prefix>_logs
WHERE network_uid = '<network-uid>'
  AND timestamp >= '<from>' AND timestamp < '<to>'
ORDER BY timestamp LIMIT 200;

-- Preserve observation order when investigating a stall or height reversal.
SELECT greptime_timestamp, pod_uid, greptime_value AS reported_height
FROM stacks_node_stacks_tip_height
WHERE network_uid = '<network-uid>' AND telemetry_uid = '<telemetry-uid>'
  AND source = 'actor-native'
  AND participant_uid = '<participant-uid>'
  AND greptime_timestamp >= '<from>' AND greptime_timestamp < '<to>'
ORDER BY greptime_timestamp LIMIT 500;

-- Distinct observed object versions; keep deletion separate from snapshots of the same version.
-- The CASE also protects parsing if the backend evaluates expressions before filtering gap records.
WITH versions AS (
  SELECT object_uid, event_type, timestamp,
         json_get_string(parse_json(CASE
           WHEN event_type IN ('Snapshot', 'ADDED', 'MODIFIED', 'DELETED') THEN body
           ELSE '{}' END), '$.metadata.resourceVersion') AS resource_version
  FROM <prefix>_objects
  WHERE network_uid = '<network-uid>'
    AND source = 'stacksnetworkparticipants.network.stacks.org/v1alpha2'
    AND event_type IN ('Snapshot', 'ADDED', 'MODIFIED', 'DELETED')
    AND timestamp >= '<from>' AND timestamp < '<to>'
)
SELECT object_uid, event_type, resource_version,
       min(timestamp) AS first_observed_at, max(timestamp) AS last_observed_at
FROM versions
GROUP BY object_uid, event_type, resource_version
ORDER BY first_observed_at LIMIT 200;
