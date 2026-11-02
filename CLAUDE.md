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
- Standard `kubernetes.Interface` for core resources (Namespaces, PVCs)
- client-go's `dynamic.Interface` for ActionSets, with the few fields kan-brewer uses mirrored in `internal/sync/actionset.go`

Both support in-cluster and local kubeconfig configurations.

**Do not import `github.com/kanisterio/kanister`.** govulncheck flags every importer with GO-2024-3080, a module-wide
advisory about the Kanister operator chart's RBAC that has no fixed version, and the module has no `v`-prefixed tags,
so Go only sees pseudo-versions that rank below an old `v0.113.0-alpha.1` tag. If a new ActionSet field is needed,
add it to `actionset.go`.

## Development Commands

### Toolchains (mise)

Toolchain versions (Go, golangci-lint) are pinned in `.tool-versions` and managed by [mise](https://mise.jdx.dev/). Run all toolchain commands through mise to ensure the correct versions are installed and used:

```bash
# Install pinned toolchains
mise install

# Run any toolchain command
mise exec -- go build ...
mise exec -- golangci-lint run ./...
```

When updating the Go version, keep `.tool-versions`, the `go` directive in `go.mod`, and the builder image in `Dockerfile` in sync (CI builds via Docker, so the Dockerfile is the only place CI picks up the Go version). golangci-lint must support the Go version in use; bump it together with Go.

CI runs neither tests nor a vulnerability scan; run both before tagging:

```bash
mise exec -- go test -race ./...
mise exec -- go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

### Building

```bash
# Local build
mise exec -- go build -o kan-brewer cmd/kan-brewer.go

# Build with version info (matches CI pattern)
mise exec -- go build -ldflags "-s -w -X github.com/haimgel/kan-brewer/internal/config.release=v1.0.0 -X github.com/haimgel/kan-brewer/internal/config.commit=$(git rev-parse --short HEAD) -X github.com/haimgel/kan-brewer/internal/config.date=$(git log -1 --format=%cI)" -o kan-brewer cmd/kan-brewer.go

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

The chart's defaults must pass PodSecurity `restricted`. To check, dry-run the rendered pod template as a bare Pod
(`kubectl apply --dry-run=server`) in a namespace labelled `pod-security.kubernetes.io/enforce: restricted`; a CronJob
there only draws warnings.

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

**Releasing:** push a `vX.Y.Z` tag on `main`. CI packages the chart with both `version` and `appVersion` set to
`X.Y.Z`, so `Chart.yaml` keeps its `0.0.0` placeholders. Bump the minor version when chart values are added or
renamed.

## Key Design Patterns

- **ActionSet Naming**: Uses `GenerateName` with pattern `auto-{blueprint}-{resource}-` for automatic unique names
- **Cleanup Strategy**: Groups by `GenerateName` prefix to retain N most recent successful ActionSets per backup type
- **Version Injection**: Build-time ldflags inject version/commit/date into `internal/config/version.go`
- **Single-run Design**: Executes once per invocation (CronJob handles scheduling)
