# Setup Server Update-In-Place Feature

**Date:** 2026-07-07
**Status:** Approved

## Context

Users deploying ccrouter on remote Linux servers (Alibaba Cloud SAS instances)
currently update manually: SCP a binary, stop the service, copy, chmod, start,
and check status. This is error-prone and tedious. The goal is to make
`setup server` detect an existing installation and offer a one-command binary
update flow that auto-downloads the latest release, verifies it, and atomically
swaps the binary.

## Design

### Detection (`internal/svcinstall/detect.go`)

New function `Detect() InstallationInfo`:

```go
type InstallationInfo struct {
    Found          bool
    Scope          Scope
    UnitPath       string // /etc/systemd/system/ccrouter.service or ~/.config/systemd/user/ccrouter.service
    Active         bool   // from systemctl is-active
    BinaryPath     string // extracted from ExecStart= in unit file
    CurrentVersion string // from running installed binary --version
}
```

- Checks both system and user unit paths
- Parses `ExecStart=` from the unit file to get the binary path
- Uses `buildSystemctlCommandForProd` for scope-aware systemctl calls
  (handles user-scope under sudo)
- Runs the installed binary with `--version` to get `CurrentVersion`

### Version parsing (`internal/version/version.go`)

Add `TagOnly() string` method:
- Strips build-hash suffix: `v0.3.1-2661291a` → `v0.3.1`
- `dev-*` returns `"dev"` (always updatable)

### Updater package (`internal/updater/`)

**`updater.go`** — Types + orchestrator:

```go
type UpdateOptions struct {
    Force    bool   // skip version comparison
    Version  string // specific tag (empty = latest)
    NoVerify bool   // skip checksum verification
    Mirror   string // GITHUB_MIRROR override
}

type UpdateResult struct {
    OldVersion string
    NewVersion string
    BinaryPath string
    Status     string // "updated", "already-latest", "rolled-back"
}
```

**`release.go`** — GitHub release fetching:
- Query `GET /repos/{owner}/{repo}/releases/latest` via GitHub API
- Mirror-aware: reads `GITHUB_MIRROR` env var as default,
  `UpdateOptions.Mirror` as override
- Falls back to direct `api.github.com` if mirror fails
- Constructs archive name from OS/arch detection (linux/darwin × amd64/arm64)
- Downloads tarball + `checksums.txt` to temp dir with progress reporting
- Verifies SHA-256 against checksums file (best-effort if file missing)
- Extracts binary, runs `<temp_binary> --version` to verify it executes

**UpdateInPlace flow:**

1. `svcinstall.Detect()` — bail if not found
2. Fetch latest release tag from GitHub API
3. Compare `TagOnly(CurrentVersion)` vs `tag_name` — skip if same (unless `--force`)
4. Download tarball to temp dir, verify checksum, extract binary
5. Verify extracted binary runs (`--version`)
6. **Stop service** (only after verification succeeds)
7. **Backup** current binary to `<path>.bak.<timestamp>`
8. **Atomic replace**: `sudo install -m 0755 <temp> <target>`
9. **Start service**
10. **Poll** up to 10s for `active (running)` state
11. If service fails → auto-rollback: restore `.bak`, restart, log warning
12. Clean up temp dir
13. Return `UpdateResult`

### CLI integration

**`internal/cli/update.go`** — New `ccrouter update` command:

```
ccrouter update [--force] [--version vX.Y.Z] [--dry-run] [--no-verify]
```

Calls `updater.UpdateInPlace()` directly, no wizard steps.

**`internal/cli/setup.go`** — Detection branch at top of `runSetupServer()`:

Before `collectAnswers()`:
1. Call `svcinstall.Detect()`
2. If found, display installation info
3. Present 3 choices:
   - "Update binary to latest release" → `updater.UpdateInPlace()`, return
   - "Full reconfiguration" → continue to existing 5-step wizard
   - "Cancel" → return nil

### Reused utilities

- `svcinstall.buildSystemctlCommandForProd` — scope-aware systemctl
- `svcinstall.DefaultServiceUser` — `"ccrouter"`
- `configwizard.EffectiveHomeDir()` — SUDO_USER resolution
- `setupprompt.Prompt` — interactive choices
- `version.TagOnly()` — version comparison
- `GITHUB_MIRROR` env var — same convention as `scripts/install.sh`

### Edge cases

| Case | Handling |
|------|----------|
| No existing install | Detection returns nothing; wizard continues unchanged |
| Already on latest | Print message, skip unless `--force` |
| Download fails | Service never stopped |
| New binary crashes after restart | Auto-rollback to `.bak` after 10s poll |
| Service already stopped | Proceed (stop is no-op) |
| Dev build (`dev-*`) | Always proceeds |
| Checksums file missing | Warn, continue (best-effort) |
| Mirror API failure | Fallback to direct `api.github.com` |
| Pre-release on GitHub | Skipped by default |

## Files to create

| File | LOC (est.) | Purpose |
|------|-----------|---------|
| `internal/svcinstall/detect.go` | ~80 | `Detect()` function, unit file parsing |
| `internal/updater/updater.go` | ~50 | Types, `UpdateInPlace()` orchestrator |
| `internal/updater/release.go` | ~150 | GitHub API, download, verify |
| `internal/cli/update.go` | ~50 | `ccrouter update` cobra command |
| `internal/svcinstall/detect_test.go` | ~60 | Tests (mock systemctl) |
| `internal/updater/release_test.go` | ~80 | Tests (mock HTTP server) |

## Files to modify

| File | Change |
|------|--------|
| `internal/version/version.go` | Add `TagOnly()` method |
| `internal/cli/setup.go` | Add detection branch at top of `runSetupServer()` |
| `internal/cli/root.go` | Register `update` command |

## Verification

1. Unit tests: `go test ./internal/svcinstall/... ./internal/updater/...`
2. Integration: `ccrouter setup server --dry-run` with existing installation
3. End-to-end: deploy a version, run `ccrouter update`, verify binary swapped
   and service restarted
4. Rollback: deploy a bad binary, verify auto-rollback restores previous version
