# Stacks evidence tool

Read-only, bounded Greptime queries, separate from workload submission and network
orchestration. Build and pipe exactly one JSON request:

```bash
GOWORK=off go -C tools/stacks-evidence build -o stacks-evidence .
stacks-evidence export < query.json > result.json
```

```json
{
  "endpoint": "http://127.0.0.1:14000",
  "authorization": "Basic <reader-credential>",
  "table": "stacks_<recording>_objects",
  "timeColumn": "timestamp",
  "networkUID": "00000000-0000-0000-0000-000000000000",
  "from": "2026-09-15T00:00:00Z",
  "to": "2026-09-15T01:00:00Z",
  "limit": 1000
}
```

Windows are half-open, limited to 24 hours, with at most 10,000 rows and a 64 MiB
response. `participantUID` is optional. The time column must be `timestamp` or
`greptime_timestamp`. Redirects are refused. Authorization is input-only; protect
input files and use a backend reader credential. A row limit does not bound backend
scan memory. This single-query command does not claim complete evidence coverage.
