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

## Development

### Prerequisites

- Go 1.25 or later ([Install Go](https://go.dev/doc/install))
- Make (optional, for using Makefile commands)

### Development Setup

```bash
# Install dependencies
go mod download

# Run locally
go run main.go --help
```

### Building

```bash
# Build binary
make build
```

Binary will be created in the `./build` directory.

### Code Quality

```bash
# Run all quality checks
make check-quality

# Individual commands
make lint    # Run linter
make fmt     # Format code
make vet     # Run go vet
```

### Cleaning Up

To clean up build artifacts and temporary files, run:

```bash
make clean
```

## Contributing

### Commit Message Format

This project uses [Conventional Commits](https://www.conventionalcommits.org/) 
with Angular preset for automated versioning and releases. 

#### Commit Message Structure

```
<type>: <description>

[optional body]

[optional footer(s)]
```

#### Supported Types and Release Impact

| Type | Description | Release Impact |
|------|-------------|----------------|
| `feat` | New feature | Minor version bump |
| `fix` | Bug fix | Patch version bump |
| `perf` | Performance improvement | Patch version bump |
| `refactor` | Code refactoring | Patch version bump |
| `chore` | Maintenance tasks | Patch version bump |
| `docs` | Documentation changes | Patch version bump |
| `style` | Code style changes | Patch version bump |
| `test` | Test changes | Patch version bump |

#### Breaking Changes

For breaking changes, add `BREAKING CHANGE:` in the commit footer or use `!` after the type/scope:

```
feat! : drop support for running on Kubernetes v1.16

BREAKING CHANGE: The minimum supported Kubernetes version is now v1.17
```

This will trigger a major version bump.

#### Examples

```bash
# Feature addition (minor release)
feat: add support for custom installation directory

# Bug fix (patch release)
fix: resolve service startup issue on OpenShift

# Breaking change (major release)
feat! : remove support for local binary execution

BREAKING CHANGE: The tool will no longer support local binary execution. Users must use the tool as Kubernetes Job.
```

## Release Process

> [!IMPORTANT]
> A release of the Ingress Migration Tool is not coupled with the main FlowFuse release process. 

To release a new version of the Ingress Migration Tool, follow these steps:
1. Ensure all changes are committed and follow the commit message format outlined above.
2. Manually trigger the [Installer Release](https://github.com/FlowFuse/ingress-migration-tool/actions/workflows/release.yaml) workflow 
3. The worflow will:
    * Calculate the new version based on commit messages since the last release
    * Build and push the Docker image to the Docker Hub with the new version tag
    * Create a new release on GitHub with the changelog