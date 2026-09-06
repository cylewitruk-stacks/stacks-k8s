# Network API module

This types-only Go module defines the versioned `network.stacks.org` and
`bitcoin.stacks.org`, `stacks.stacks.org`, and `actions.stacks.org` APIs used
by the network operator and future typed consumers. It contains Kubernetes API
types, scheme registration, and generated deepcopy implementations; it must
not depend on controller-runtime, client-go, or controller implementations.
Its minimum supported Go version follows its Kubernetes API dependencies;
direct repository development uses the preferred toolchain declared in
`go.mod`.

Run generation and verification from the repository root:

```bash
make generate
make api-verify
make module-policy-verify
```

The isolated generator module under `tools/` writes CRDs directly into the
network operator chart. Release this module with a subdirectory-prefixed tag,
such as `apis/network/v0.1.0`, before releasing an operator module that
requires that version.
