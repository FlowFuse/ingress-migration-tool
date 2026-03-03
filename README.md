# Ingress Migration Tool

A small Go utility to help migrate Kubernetes Ingress resources between ingress controllers. It can copy existing ingresses to a new `ingressClassName`, and later clean up old ingresses while renaming the copied ones back to their original names.

## Modes

- `copy`: Create a new ingress for each existing ingress, using a name suffix and a new ingress class.
- `cleanup`: Delete ingresses that still use the old ingress class and rename the suffixed ingresses back to their original names.

## Configuration

The tool is configured via environment variables:

- `TARGET_NAMESPACE` (default: `default`)
- `MODE` (`copy` or `cleanup`, default: `copy`)
- `OLD_INGRESS_CLASS` (required for `cleanup`)
- `NEW_INGRESS_CLASS` (required)
- `NAME_SUFFIX` (default: `-copy`)
- `DRY_RUN` (`true`/`false`, default: `false`)
- `CONCURRENCY` (default: `100`)

## Build

Local build:

```bash
make build
```

Container image build:

```bash
make build-docker
```

## Notes

- The tool attempts in-cluster config first, then falls back to `KUBECONFIG` or `~/.kube/config`.
- It skips ingresses that already use the target class or already have the configured suffix.
