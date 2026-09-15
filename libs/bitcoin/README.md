# Bitcoin RPC library

Independent Go module with no Kubernetes or operator dependencies. Consumers require
`github.com/cylewitruk-stacks/stacks-k8s/libs/bitcoin` and use a repository-local
`replace` directive when developing here. Repository MIT licensing applies.

`rpc` supplies authenticated JSON-RPC calls, method/error vocabulary, regtest
preflight and single-block generation helpers. Calls require an explicit endpoint
and caller context. Connection establishment is bounded to three seconds; callers
must set their own response deadline or cancellation policy. Responses are limited
to 64 KiB and must match the request ID. Errors exclude server response bodies and
credential-bearing endpoints. Redirects, proxy discovery and connection reuse are
disabled; request bodies cannot be replayed by the HTTP transport.

The library does not provision credentials, retry mutations, decide target admission,
poll on a schedule or account for execution. A timeout does not establish that a
server-side mutation stopped. Callers retain their own uncertainty and receipt policy.
The convenience preflight explicitly requires regtest; `Call` itself has no chain policy.
