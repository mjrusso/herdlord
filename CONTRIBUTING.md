# Contributing

Use Nix for the development shell:

```sh
nix develop
```

The default shell includes everything in the CI shell (`go`, `git`, `just`,
`goreleaser`, `golangci-lint`, and `actionlint`) plus `gopls`, `gotools`, and
platform-specific notification helpers. `nix develop .#ci` is the minimal
shell that CI uses.

Before committing changes, run:

```sh
just ci
```

Build a local binary with `just build`. Use a disposable targets file so tests
do not modify your normal Herdlord configuration:

```sh
./bin/herdlord --config /tmp/herdlord-targets.json
```

See [Test with multiple local Herdr sessions](#test-with-multiple-local-herdr-sessions)
for an end-to-end local setup.

## Test with multiple local Herdr sessions

Herdr's named sessions provide separate local servers, so you can test several
Herdlord targets without remote machines.

The demo recipes create two healthy named sessions, an intentionally offline
target, and a disposable Herdlord configuration. Use three terminals:

```sh
just demo-session a
just demo-session b
just demo
```

Run the session commands in separate terminals to create workspaces and agents.
The dashboard uses a short polling interval and is ready for manual testing or
screenshots. Stop and restart one session to exercise backoff and recovery:

```sh
just demo-toggle b
just demo-toggle b
```

The first command stops session B; the second restarts it. Remove the
environment with:

```sh
just demo-down
```

Demo state defaults to `.herdlord-test/demo` in the repository. Set
`HERDLORD_DEMO_DIR` to use another location. The setup refuses to claim an
existing session with one of its reserved names. Teardown deletes only sessions
recorded in its ownership file and retains that file if cleanup fails.

The equivalent manual setup follows.

Start two named sessions in separate terminals:

```sh
herdr --session herd-a
```

```sh
herdr --session herd-b
```

Create workspaces or start agents in each session, then confirm that both
servers are available:

```sh
herdr session list
```

Configure the following targets with a disposable targets file:

| Name      | Prefix                     | Attach prefix              |
|-----------|----------------------------|----------------------------|
| `local-a` | `env HERDR_SESSION=herd-a` | `env HERDR_SESSION=herd-a` |
| `local-b` | `env HERDR_SESSION=herd-b` | `env HERDR_SESSION=herd-b` |
| `default` | empty                      | empty                      |

The equivalent CLI commands are:

```sh
./bin/herdlord --config /tmp/herdlord-targets.json targets add local-a \
  --prefix 'env HERDR_SESSION=herd-a'
./bin/herdlord --config /tmp/herdlord-targets.json targets add local-b \
  --prefix 'env HERDR_SESSION=herd-b'
./bin/herdlord --config /tmp/herdlord-targets.json targets add default
```

Then run Herdlord with a shorter poll interval:

```sh
./bin/herdlord \
  --config /tmp/herdlord-targets.json \
  --interval 500ms \
  --timeout 3s
```

The target prefix sets `HERDR_SESSION` before Herdlord invokes Herdr. You can
check the same resolution outside the dashboard:

```sh
env -u HERDR_SOCKET_PATH \
  -u HERDR_CLIENT_SOCKET_PATH \
  HERDR_SESSION=herd-a \
  herdr status
```

To test failure, backoff, and recovery, stop one session while Herdlord is
running, then start it again:

```sh
herdr session stop herd-b
herdr --session herd-b
```

When testing is complete, stop and delete the sessions and remove the
disposable targets file:

```sh
herdr session stop herd-a
herdr session stop herd-b
herdr session delete herd-a
herdr session delete herd-b
rm -f /tmp/herdlord-targets.json /tmp/herdlord-targets.json.lock
```

## justfile targets

| Target                        | Purpose                                                           |
|-------------------------------|-------------------------------------------------------------------|
| `just build`                  | Build `bin/herdlord`.                                             |
| `just docs`                   | Regenerate the Cobra command reference.                           |
| `just docs-check`             | Verify that generated command documentation is current.           |
| `just test`                   | Run `go test ./...`.                                              |
| `just race`                   | Run the Go tests with the race detector.                          |
| `just vet`                    | Run `go vet ./...`.                                               |
| `just lint`                   | Run `golangci-lint` and `actionlint`.                             |
| `just tidy-check`             | Verify that `go mod tidy` produces no changes.                    |
| `just scripts-check`          | Syntax-check the release scripts.                                 |
| `just skill-check`            | Validate the embedded agent skill against the command tree.       |
| `just smoke`                  | Build the binary and run its version and help commands.           |
| `just demo`                   | Set up and open the local demo dashboard.                         |
| `just demo-session a`         | Set up and attach to demo session `a` or `b`.                     |
| `just demo-toggle b`          | Stop or restart a session to test failure and recovery.           |
| `just demo-down`              | Delete owned demo sessions and disposable state.                  |
| `just release-snapshot-check` | Build, verify, and smoke-test snapshot release archives.          |
| `just nix-check`              | Check the flake, build `.#herdlord`, and run the Nix binary.      |
| `just release-prep vX.Y.Z`    | Validate release notes and run the local release checks.          |
| `just check`                  | Run the source, test, lint, script, smoke, and GoReleaser checks. |
| `just ci`                     | Run `check`, the release snapshot, and the Nix checks.            |
| `just dev`                    | Alias for `just check`.                                           |
| `just clean`                  | Remove `bin/`, `dist/`, and `result`.                             |

## Generated command docs

`docs/commands/` is generated by `cmd/gen-docs/main.go` from the Cobra command
tree. Do not edit those files by hand. Run `just docs` after changing commands,
flags, or help text; `just docs-check` and CI fail when the generated tree
drifts from the command definitions.

## Internal package boundaries

- `internal/target` owns target definitions, prefix parsing, and persistence.
- `internal/targetmgr` coordinates target health checks and configuration changes.
- `internal/cli` owns Cobra commands and text and JSON output.
- `internal/display` sanitizes untrusted values at human-readable terminal
  output boundaries while leaving JSON protocol data unchanged.
- `internal/fleet` owns concurrent one-shot collection across targets.
- `internal/herdr` owns Herdr command construction, status parsing, snapshot
  decoding, and agent output reads.
- `internal/poll` owns polling, timeouts, target states, and backoff.
- `internal/ui` owns the Bubble Tea model, rendering, input, and attach lifecycle.
- `internal/buildinfo` owns version metadata injected during builds.
- `internal/skill` owns the embedded version-matched agent instructions.

Transport-specific behavior does not belong in these packages. A target prefix
must remain a generic argv that can carry a command to any environment that
runs Herdr.

## Test conventions

Use Go's standard `testing` package. Prefer table tests for parsing and state
transitions. Use executable stubs for Herdr commands so tests do not require a
network, remote machine, or running Herdr server.

Keep these areas covered:

- quoted, escaped, and empty target prefixes;
- duplicate-name validation and atomic persistence;
- Herdr status and snapshot decoding;
- supported, too-old, and newer untested protocol versions;
- command construction and inherited socket-variable removal;
- target state transitions and backoff reset;
- non-zero exits, stderr, malformed JSON, and command timeouts;
- terminal-control sanitization in human-readable output and raw JSON
  preservation;
- table sorting and rendering for empty and failing targets;
- focused-output revision caching;
- pause, resume, refresh, deletion, and attach command selection;
- concurrent target mutations and live TUI configuration reconciliation.

Fixture commands should print recorded responses for the supported Herdr
protocols: Herdr 0.8.0 protocol 19 and Herdr 0.8.2 protocol 20.
Failure fixtures should support non-zero exits, stderr output, malformed JSON,
and a delay longer than the configured timeout.

The optional integration test uses an empty target prefix against the local
Herdr installation. Keep it separate from deterministic unit tests so the
normal test suite does not depend on local server state.

Run the race detector when changing poller or Bubble Tea message flow:

```sh
just race
```

## Nix package

`flake.nix` exposes `packages.herdlord` through `pkgs.buildGoModule`:

- `pname = "herdlord"`;
- `subPackages = [ "cmd/herdlord" ]`;
- release metadata is injected through `internal/buildinfo`;
- `packages.default` and `apps.default` point to Herdlord.

After Go dependency changes, update `vendorHash` in `flake.nix`:

1. Set `vendorHash = lib.fakeHash;` temporarily.
2. Run `nix build .#herdlord`.
3. Copy the fixed-output hash from the Nix error into `flake.nix`.

Two development shells are exposed:

- `devShells.ci` contains the tools used by GitHub Actions.
- `devShells.default` adds Go editor tools and platform notification helpers.

## CI

GitHub Actions installs Nix in every job. Four jobs run on pull requests and
pushes to `main`:

1. `check` runs `nix develop .#ci -c just check` on Ubuntu and macOS.
2. `release-snapshot` runs the GoReleaser snapshot check on Ubuntu.
3. `race` runs `nix develop .#ci -c just race` on Ubuntu.
4. `nix` checks and builds the flake on Ubuntu and macOS, then runs the packaged
   binary.

Each job uses `actions/checkout@v5`,
`nixbuild/nix-quick-install-action@v33`, and
`nix-community/cache-nix-action@v6`. Nix caches are keyed by the operating
system and the hash of the Nix files and `flake.lock`.

## Changelog

`CHANGELOG.md` is curated by hand. Keep `## Unreleased` at the top and add
user-facing changes there as they land. Internal maintenance can be omitted
unless it changes installation, artifacts, compatibility, or operator
workflows.

Before tagging a release, move the relevant entries into a section with this
exact form:

```md
## v0.1.0 - YYYY-MM-DD

- Add x.
```

The release workflow rejects a tag without a matching, dated, non-empty
section. It passes that section to GoReleaser as the GitHub release notes.

## Release process

Releases are tag-driven. To cut `vX.Y.Z`, start from an up-to-date `main`, set
`VERSION` to `X.Y.Z`, prepare the changelog, commit those changes, and run the
local release checks:

```sh
git switch main
git pull --ff-only
$EDITOR CHANGELOG.md
printf '%s\n' X.Y.Z > VERSION
git add CHANGELOG.md VERSION
git commit -m "Prepare vX.Y.Z release"
just release-prep vX.Y.Z
git push origin main
```

If release preparation passes, create and push an annotated tag:

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

Tags matching `v*` trigger `.github/workflows/release.yml`, which:

- verifies that the tag commit is reachable from `origin/main`;
- runs `just check`;
- validates and extracts the matching changelog section;
- creates a draft release with GoReleaser;
- verifies archive contents and SHA-256 checksums;
- smoke-tests the archive for the workflow host;
- publishes the verified draft.

GoReleaser builds `herdlord` with `CGO_ENABLED=0` for macOS and Linux on AMD64
and ARM64. Each archive includes `LICENSE`, `README.md`, `CHANGELOG.md`, and the
binary.

Before the first release and other major releases, run the automated checks and
manually verify the disposable multi-session environment on each available host
platform. Cover local and remote targets, an unreachable target, protocol skew,
pause and resume, external CLI edits while the TUI is open, recent output,
interactive attach and return, and narrow and short terminals. Smoke-test a
release archive on macOS as well as Linux when both hosts are available.

Expected release artifacts:

```text
dist/herdlord_darwin_arm64.tar.gz
dist/herdlord_darwin_x64.tar.gz
dist/herdlord_linux_arm64.tar.gz
dist/herdlord_linux_x64.tar.gz
dist/checksums.txt
```
