# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Kan-Brewer is a Kubernetes backup scheduler for Kanister. It creates periodic Kanister ActionSets based on annotations on Namespaces and PVCs, and automatically cleans up old successful ActionSets.

## Architecture

### Core Components

- **cmd/kan-brewer.go**: CLI entry point using urfave/cli/v3
  - Accepts `--namespace` (where ActionSets are created, default: "kanister")
  - Accepts `--keep-successful` (number of successful ActionSets to retain, default: 3)
  - Injects version info via ldflags at build time

- **internal/sync/sync.go**: Main synchronization logic
  - `Synchronizer` struct handles all Kubernetes client interactions
  - Creates ActionSets for annotated Namespaces and PVCs
  - Groups ActionSets by `GenerateName` for cleanup
  - Deletes old successful ActionSets (keeps only N most recent per group)

- **internal/config/config.go**: Configuration and constants
  - Annotation name: `kan-brewer.haim.dev/kanister-blueprints` (comma-separated blueprint list)
  - Managed-by label: `app.kubernetes.io/managed-by=kan-brewer`

### Workflow

1. Discover all Namespaces and PVCs in cluster
2. For each resource with the blueprint annotation:
   - Parse comma-separated blueprint names
   - Create ActionSet with name pattern: `auto-{blueprint}-{namespace}[-{pvc}]-`
3. Clean up old ActionSets:
   - Only those managed by kan-brewer (via label)
   - Only successful ones (`state: complete`)
   - Group by `GenerateName`, keep N most recent per group

### Kubernetes Clients

The app uses two Kubernetes clients:
- Standard `kubernetes.Clientset` for core resources (Namespaces, PVCs)
- Kanister `kanisterclient.Clientset` for ActionSets (custom resources)

Both support in-cluster and local kubeconfig configurations.

## Development Commands

### Building

```bash
# Local build
go build -o kan-brewer cmd/kan-brewer.go

# Build with version info (matches CI pattern)
go build -ldflags "-s -w -X github.com/haimgel/kan-brewer/internal/config.release=v1.0.0 -X github.com/haimgel/kan-brewer/internal/config.commit=$(git rev-parse --short HEAD) -X github.com/haimgel/kan-brewer/internal/config.date=$(git log -1 --format=%cI)" -o kan-brewer cmd/kan-brewer.go

# Cross-platform builds (using goreleaser config in dist/)
goreleaser build --snapshot --clean
```

### Docker

```bash
# Build multi-arch image (as done in CI)
docker buildx build --platform linux/amd64,linux/arm64 -t kan-brewer:local .
```

### Helm

```bash
# Lint chart
helm lint helm/kan-brewer

# Test package
helm package helm/kan-brewer --version 0.99.99 --app-version 0.99.99

# Install locally
helm install kan-brewer helm/kan-brewer \
  --namespace kanister \
  --set cronJob.schedule="45 4 */1 * *" \
  --set keepSuccessfulActionsets=3
```

### Running Locally

The app runs once and exits (designed for CronJob):

```bash
./kan-brewer --namespace kanister --keep-successful 3
```

## CI/CD

GitHub Actions workflow (`.github/workflows/package.yaml`):
- Builds multi-arch Docker images (amd64, arm64)
- Signs images with cosign (tags only)
- Publishes Helm charts to GHCR OCI registry
- Uses Docker buildx with QEMU for cross-compilation
- Tags: branches, PRs, semver (on tags)

## Key Design Patterns

- **ActionSet Naming**: Uses `GenerateName` with pattern `auto-{blueprint}-{resource}-` for automatic unique names
- **Cleanup Strategy**: Groups by `GenerateName` prefix to retain N most recent successful ActionSets per backup type
- **Version Injection**: Build-time ldflags inject version/commit/date into `internal/config/version.go`
- **Single-run Design**: Executes once per invocation (CronJob handles scheduling)
