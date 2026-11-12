# Cookbook

Short questions, short answers. Full flags: [USAGE.md](../../USAGE.md).

## How do I install or update harnessator itself?

```console
$ curl -fsSL https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.sh | bash
$ harnessator version
harnessator-install: done. → harnessator v0.11.0-beta
```

Re-running the same line updates to the latest release; `--version` pins
and `--dir` relocates. Already installed? `harnessator selfupdate`
(sha256-verified, smoke-tested, previous binary kept as `*.old`) or
`--check-only` to just ask. Installs older than v0.6.0-beta re-run the
installer once to acquire `selfupdate`; after that it carries itself. Windows: `irm https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.ps1 | iex`.
Every download is sha256-verified against the release's checksums file -
the installer refuses on mismatch, same as the tool refuses on digest
drift.

## How do I consume public skill libraries?

Jesse Vincent's [superpowers](https://github.com/obra/superpowers)
(TDD, debugging, planning - 15 battle-tested skills, MIT) ships as a
pinned source through the
[harvest bridge](https://github.com/aignerdevelopment/superpowers):
byte-identical skill directories, upstream LICENSE carried, every
component's origin naming the exact upstream commit.

```sh
git init -b main
harnessator init --targets claude,opencode .
harnessator source add https://github.com/aignerdevelopment/superpowers.git --ref '~v7'
harnessator enable superpowers:default
harnessator setup
ls .claude/skills
```

```console
$ harnessator why skill/test-driven-development
superpowers:skill/test-driven-development enabled by: template superpowers:default [via project]
  template default [via project] of source superpowers selects it
$ harnessator check --json
{"ok":true,"problems":[],"warnings":[]}
```

`superpowers:core` (4 skills) or `superpowers:default` (all 15); pin
ranges work like anywhere else, and a team layer source-add governs the
whole org at once. Honest limit: the bridge distributes the *files* -
upstream's session-start bootstrap (their plugin's auto-trigger) is not
carried; skills load through each harness's native discovery. If the
upstream one day ships its own manifest, repoint `source add` at
obra/superpowers and retire the bridge.

Rules work the same way, at registry scale. The
[cursor-rules](https://github.com/aignerdevelopment/cursor-rules) bridge
carries 241 curated Cursor rule files (CC0, upstream LICENSE travels)
**as per-stack profiles** - because 241 rules enabled blindly is noise,
not governance:

```sh
harnessator source add https://github.com/aignerdevelopment/cursor-rules.git --ref '~v0.1'
harnessator enable cursor-rules:essentials
harnessator setup
```

```console
$ harnessator why rule/git
cursor-rules:rule/git enabled by: template cursor-rules:essentials [via project]
  template essentials [via project] of source cursor-rules selects it
$ harnessator check --json
{"ok":true,"problems":[],"warnings":[]}
```

Profiles: `essentials`, `web-react`, `python-data`, `backend-go`,
`devops`, `ml-core`; individual rules enable like any component
(`cursor-rules:rule/terraform`). Want your own bridge? Same pipeline we
used: `harvest --dry-run` the checkout, **review** every sanitize
finding (bridge ships the reviewed list as `HARVEST-NOTES.md`), curate
profiles instead of a mega-default, and let the tool carry the license
and provenance.

At true registry scale: [copilot-skills](https://github.com/aignerdevelopment/copilot-skills)
- 455 skills harvested from
[github/awesome-copilot](https://github.com/github/awesome-copilot)
(MIT, LICENSE carried). Its manifest is a **pointer**: `[discover]`
registers every skill directory live and `UPSTREAM.md` records the
pinned ref, commit and license - so the registration cannot drift, and
an upstream release reaches you as a new bridge tag, never a silent
change. 455 skills is a directory, not a daily toolchain - enable a
stack profile, never `:default` with intent you don't have:

```sh
harnessator source add https://github.com/aignerdevelopment/copilot-skills.git --ref '~v0.2'
harnessator enable copilot-skills:agent-dev
harnessator setup
```

```console
$ harnessator why skill/agent-architecture
copilot-skills:skill/agent-architecture enabled by: template copilot-skills:agent-dev [via project]
  template agent-dev [via project] of source copilot-skills selects it
$ harnessator search chrome
off  copilot-skills:skill/chrome-devtools — Expert-level browser automation, debugging, and performance analysis using Chrome DevTools MCP. …
```

Profiles: `quality-security`, `agent-dev`, `azure-dotnet`,
`python-stack`, `web-frontend` - keyword-built against the real id
list, membership reviewed. `search` before you enable; the registry is
wide on purpose.

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

## My gateway's models moved on - does anyone notice?

Models rot silently: a catalog alias pointing at a retired model
renders a harness that fails at runtime, and new reachable models stay
invisible. `harnessator tune` is the guided answer - it walks config,
pins, auth env vars, policy/dialect consistency, probes every API
style's live `/models`, and reconciles the catalog (drops stale, adds
new, you select which). Changes land in a dev overlay and go upstream
via `contribute` - pinned checkouts are never edited.

```sh
harnessator tune --check-only
```

```console
$ harnessator tune --config harnessator.toml --check-only
[5/7] gateway probe
  ok     openai-compatible: 2 models reachable at https://gw.acme/v1
[6/7] model catalog
  note   stale alias old -> gone-me is no longer reachable
  note   new model brand-new reachable (suggested alias "brand-new")
  catalog     1 stale, 1 new (check-only)
```

Interactive terminals get per-step offers (including pasting a missing
env value for the current run only - values are never stored);
`--yes` applies the recommendations for automation, `--no-probe` keeps
the checkup fully offline.

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
