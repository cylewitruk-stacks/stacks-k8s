# Releases

The two operators are independently versioned products in one repository.
Each release updates its chart `version`, chart `appVersion`, image tag, and
operator release notes together.

Use component-prefixed product tags:

```text
stacks-network-operator/v0.1.0
stacks-observability-operator/v0.1.0
```

If a Go module is published for external import, its semantic-version tag must
match its repository subdirectory instead:

```text
operators/network/v0.1.0
operators/observability/v0.1.0
```

Before tagging:

1. run `make verify`, `make vuln`, and `make docker-check`;
2. build and publish an immutable multi-architecture image;
3. set the chart image tag to the released image version;
4. install the packaged chart in a clean cluster and exercise its examples;
5. publish each chart independently as an OCI artifact.

Repository placement does not imply compatibility across arbitrary operator
versions. Document the network API and inventory-contract versions accepted by
each observability release.
