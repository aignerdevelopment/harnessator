# Contributing to harnessator

Short version first: keep every gate green, update the docs in the same
commit, and open an issue before touching anything that is a published
contract (file formats, resolution semantics, store layout).

## Gates

Everything CI runs must pass locally before you push:

```sh
make fmt-check vet test   # gofmt, go vet, go test ./...
make drift                # docs drift gate (commands/flags vs USAGE.md + man/)
make man-check            # roff validity (needs groff)
make cross                # windows/darwin cross-compile from linux
```

CI (`.github/workflows/ci.yml`, mirrored by the development pipeline) runs
the same matrix. Note that `make test` already includes the drift gate -
`internal/cli/docs_test.go` fails the build when any command or flag is
missing from `USAGE.md` or the man pages.

## Documentation duty (enforced, not folklore)

Every user-facing change ships together with its docs, in the same
commit: `README.md` when the pitch or claims change, `USAGE.md` for flags,
formats and behavior, `man/` for the roff mirror of the same,
`BUILDING.md` for build/packaging changes. The drift gate catches the
mechanical half (flags/commands); keeping prose *true* is on you.

## Code rules

- **Stdlib first.** Current dependencies: `BurntSushi/toml`,
  `golang.org/x/mod`, `golang.org/x/sys`. New ones need a concrete reason
  in the PR description.
- **Single static binary.** harnessator cross-compiles from one build for
  linux/mac/windows - no cgo, no Go `plugin`, no runtime helpers.
- **Adapters are honest.** A component a target cannot take is reported as
  a `SKIP` line - never a silent drop. Same for governance: a target
  without a dialect, or a profile without a speakable API style, says so
  loudly.
- **Governance = dialects.** New harness rendering for providers/policies
  goes into `internal/dialect` (capability = registration); a new
  protocol family is one style constant plus dialect entries - keep the
  interfaces pure data, they may become out-of-process one day.
- **Secrets never land in git or the store** - configurations carry env
  var *names* only.
- Commit style: imperative subject with scope (`Registry index: ...`,
  `MCP server: ...`), body says *why*.

## What needs an issue first

Schemas (project/lock/source manifest/provider/policy formats), ref
semantics, the store layout, and anything labeled "harnessator source
format" - these are the public contract and get frozen deliberately.
Typo fixes and internal refactors do not need one.

## License

By contributing you agree your work is licensed under the
Apache-2.0, no CLA, no DCO (yet - see issue tracker if that changes).
The project is © 2026 Roland Aigner; your contributions stay your
copyright and are licensed to everyone under Apache-2.0 by submitting
them (standard Apache §5 semantics).
