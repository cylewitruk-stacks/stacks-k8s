# Observation operator development

The one-shot reader uses `network.stacks.org/v1alpha2`: admitted generated participants,
public resolver reports and uncached Service/StatefulSet/Pod reads establish the
selected actor snapshot.
The observation owns its snapshot digest; the network publishes no aggregate
runtime inventory digest. No legacy network reader or version fallback is provided.

The reader imports shared versioned API types, never network controller runtime.
It rejects unknown admitted configuration fields before checking the complete
policy digest. It does not read Secrets, signing keys or protocol endpoints.
Configuration Secret UID and content fingerprint are attributed to public
controller reports; mounted names and public Pod annotations are checked directly.
Observation success does not authorize dispatch or imply global protocol health.

Development requires Go 1.27.1; the matching generator toolchain is pinned in
`tools/go.mod`.

Run development and chart checks from the repository root:

```bash
make verify-observability
```

Generated deepcopy code and the CRD are committed. Do not hand-edit them.
Participant identity tests include
API-server and read-only RBAC coverage. Actor protocol probes are outside this reader.
