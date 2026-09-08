# Observation operator development

The first slice has an event-driven one-shot observation reconciler, an
uncached leaf/Service/StatefulSet/Pod identity verifier, canonical digest
verification, bounded pending lifetime, and an exact RBAC validator.
Development requires Go 1.27.1; the matching generator toolchain is pinned in
`tools/go.mod`.

Run development and chart checks from the repository root:

```bash
make verify-observability
```

Generated deepcopy code and the CRD are committed. Do not hand-edit them.
Live qualification and actor protocol probes are intentionally deferred beyond
this first trust-boundary slice.
