# harnessator — building & packaging

## Prerequisites

| need | version | needed for |
|---|---|---|
| Go | ≥ 1.24 toolchain (go.mod requires 1.26; CI uses 1.27) | everything |
| git | ≥ 2.28 (`git init -b`) | gitops + the test suite |
| GNU make | any | convenience targets below |
| groff | any | `make man-check` (man page syntax validation) |
| `npx`/prettier | optional | repo formatting of md/json — not required |

No CGO, no C toolchain: builds are pure Go (`CGO_ENABLED=0`), so
cross-compilation from any host works out of the box. Runtime deps:
the **system git** is invoked (by design: native git auth/helpers/worktrees
beat a Go reimplementation). Optional CLIs for
convenience only: `glab`, `az`, `gh` (`contribute`/`source new --remote`;
without them harnessator prints the exact web URL instead).

Dependencies (go.mod): `github.com/BurntSushi/toml`,
`golang.org/x/mod/semver`, `golang.org/x/sys` (windows reparse points only).

## Native build

```sh
make                 # = build + test
make build           # bin/harnessator with version stamped from `git describe`
go build ./...       # plain equivalent (version stays "dev")
```

Version stamping (what CI does):

```sh
go build -ldflags "-s -w -X harnessator.org/harnessator/internal/cli.Version=$(git describe --tags --always --dirty)" \
     -o bin/harnessator ./cmd/harnessator
```

## Cross-compile matrix (single host builds all)

```sh
make cross        # linux/{amd64,arm64}, darwin/{amd64,arm64}, windows/amd64
```

| GOOS/GOARCH | notes |
|---|---|
| linux/amd64, linux/arm64 | plain symlinks |
| darwin/amd64, darwin/arm64 | plain symlinks (APFS fine); arm64 = Apple silicon |
| windows/amd64 | **junctions** for directory links (no admin/developer mode), **copies** for files (drift caught by `harnessator verify` digests) |

Windows/macOS CI *execution* is intentionally not wired yet (runner
postponed); the cross-builds compile-verify the code, and
`internal/platform/link_windows.go`'s reparse encoding is documented
in-source and still needs a real-machine pass.

## Testing

```sh
make test     # go vet + go test ./...  (13 test packages, ~4s, no network)
```

Tests are hermetic: git operations run against ephemeral repos under
`t.TempDir()` (file:// URLs); no network, no global state. On non-symlink
filesystems the symlink-dependent tests skip themselves. `make man-check`
renders every roff page through groff. `internal/cli/docs_test.go` is the
documentation drift gate: **every command and every flag of the CLI must be
documented in USAGE.md and the man pages or `go test ./internal/cli`
fails** - the doc gate is machine-enforced.

## Install & man pages

Prebuilt binaries: the one-line installer at the top of the
[README](README.md#install) (`install.sh` for unix/git-bash, `install.ps1` for
PowerShell) - sha256-verified against the release's checksums, no admin
needed, re-run to update. From source:

```sh
make install            # → $HOME/.local/bin/harnessator + man pages
make install PREFIX=/usr/local DESTDIR= staging/   # packaged installs
make man               # groff-validate all pages
```

Installed layout: `$(PREFIX)/bin/harnessator`,
`$(PREFIX)/share/man/man1/harnessator.1.gz`,
`…/man5/harnessator.{toml,lock}.5.gz`.

After install, `harnessator version` should print the stamp;
`man harnessator` shows the full reference (mirrored by `harnessator help`).

## Releases

```sh
make dist    # dist/harnessator_<ver>_<os>_<arch>.tar.gz (windows: .zip) + checksums.txt
```

Cut `vX.Y.Z` tags in the source repo; harnessator source pins are
**semver tags** (`~vX.Y`) so `make dist`-style artifacts and tag pins
line up. `dist/harnessator_…_checksums.txt` is sha256, one per archive.

## Repo layout (build-relevant)

```
cmd/harnessator/    main.go - thin entry (all logic below)
internal/cli/       RunIO dispatch, usage text, serve.go (+web/), tui.go,
                    web/index.html (embedded), docs_test.go (drift gate)
internal/app/       the core API every frontend binds to
internal/{config,lock,resolve,gitops,store,contenthash,platform,host,policy,provider,materialize,semverrange,schema}
man/                man1/harnessator.1 · man5/harnessator.{toml,lock}.5
Makefile            build cross test man-check man install dist fmt vet
```

## GitLab CI (`.gitlab-ci.yml`)

Linux test job (`go vet`+`go test`) and a 3-way cross-build job
(windows/amd64, darwin/arm64, darwin/amd64 → artifacts). Extend with real
macOS/Windows test runners when available — `make test` is the contract
they must keep green.

## Building on each OS (native)

- **Linux/macOS**: install Go from go.dev/dl (or distro package), then
  `make` at the repo root.
- **Windows**: install Go (winget `GoLang.Go`), git-for-windows; use the
  Makefile from git-bash (it needs `make` — `choco make`/winget) or run
  the raw commands directly:
  `go build -o bin\harnessator.exe .\cmd\harnessator`. `go test .\...`
  is the same. Symlink-dependent tests skip without developer mode;
  junction/copy platform code is exercised via `GOOS=windows go vet/build`
  + real-machine runs (see table above).

## First-time contributing

1. `make` everything green.
2. For user-visible changes: update USAGE.md + man/ + README pitch in the
   **same commit** (the drift test enforces command/flag coverage; rule
   18-docs-sync the rest).
3. `harnessator` dogfoods itself: this repo's config/lock are updated via
   `harnessator update` when its own manifests change.
