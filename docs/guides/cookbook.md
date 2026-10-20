# Cookbook

Short questions, short answers. Full flags: [USAGE.md](../../USAGE.md).

## How do I install or update harnessator itself?

```console
$ curl -fsSL https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.sh | bash
$ harnessator version
harnessator-install: done. → harnessator v0.1.0-beta
```

Re-running the same line updates to the latest release; `--version` pins
and `--dir` relocates. Windows: `irm https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.ps1 | iex`.
Every download is sha256-verified against the release's checksums file -
the installer refuses on mismatch, same as the tool refuses on digest
drift.

## How do I keep different toolchains for different kinds of work?

Per-project picks go to the **local** layer
(`harnessator enable <ref> --layer local` → `.harnessator/local.toml`,
gitignored, binds to this repo only); machine-wide favorites to the
**personal** layer (`--layer personal` → `~/.harnessator/personal.toml`,
follows you everywhere). `harnessator layers` shows both; `why` names
the file. Embedded-me and ux-me can share one corporate registry without
crosstalk.

## How do I see what's enabled and why?

```console
harnessator list              # enabled components, with provenance
harnessator list --all        # plus skipped, with the reason
harnessator why skill/foo     # which layer/selector made it active
```

## How do I pin a source to a stable version?

```console
harnessator source add https://github.com/aignerdevelopment/agentictools.git --ref ~v0.1
```

Tag ranges follow semver (`~v0.1` = within minor; `^v0.1.0` = within
major; bare `v0.1.0` = exact). Branch refs follow HEAD - use tags where
surprise patches are unwanted.

## How do I search without a project?

```console
harnessator index build       # in one managed project (one-time per update)
harnessator index search skill --category skill
harnessator index show <source>:<category>/<id>
```

The index is a rebuildable cache in the store - search and show work
with no project, no lock, no network.

## How do I add my personal favorite rules without touching the team repo?

Add a personal-layer source: a repo (or a subdirectory manifest of an
existing one) you own, registered with `--layer personal`; then
`harnessator enable mystuff:…`. The layer wins over project selections
for you alone, and `why` shows it every time.

## The source I consume just changed - what reaches me?

Nothing until you run it. Pinned by commit+digest in your lock, the
only way content reaches your project is `harnessator update` producing
a lock diff you review and commit.

## How do I move a skill into the shared registry?

If a skill lives in your project tree, [adopt the project](dev/adopt-existing-projects.md);
if it lives nowhere yet, add it to the registry repo under its category
dir and register it in the manifest (`rule`, `skill`, `prompt`,
`workflow`, `agent`, `hook`, `tool`, `mcp`, `provider`, `policy`).

## How do I run this on a laptop with no access to the corporate Git?

`harnessator` fails loudly, never silently - but pinning is local. For
air-gapped or CI-restricted setups: fetch once where you have access
(`harnessator update`), and the lock + store carry the exact content;
`setup` on the offline machine verifies digests, no fetch needed.

## How do I know an update broke something?

`harnessator check --json` before/after in your loop, or the pre-commit
hook ([ci-and-review](platform/ci-and-review.md)). Digest mismatches
name the exact component.

## Can I manage two repos from one component set?

Each project is a consumer; put the component in a source repo and
enable it in both configs. Shared content, separate pins - that's the
architecture, not a workaround.

## Where do I get an MCP server component?

Same registry model as skills: a source repo's `mcp/` directory
registered per key. Remote vs local commands both render; the
difference is per-target shape (generated `.mcp.json` for Claude Code,
`opencode.json` block for opencode, managed block for codex).

## How do I undo everything?

The generated state is links + marked blocks. `harnessator doctor`
lists what belongs to the tool; your own files are never touched -
that invariant is tested, not promised.
