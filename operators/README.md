# Operators

- [`network/`](network/) owns topology compilation and actor workloads.
- [`observability/`](observability/) performs read-only, identity-bound
  topology observations.

Each directory is an independent runtime module and container build context.
Generator dependencies are isolated in its nested `tools` module.
