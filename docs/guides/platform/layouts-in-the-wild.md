# Layouts in the wild (a bestiary)

No two repositories meet you with the same agent-configuration shape.
Over the adoption stories this project grew from - a Windows terminal
emulator, a skill collection whose maintainer declined our PR, team
corpora, a hundred-plus rule packs, your own dotfiles at 2 a.m. - the
same handful of *species* keep reappearing. This page catalogs them:
what the layout looks like, what `harnessator scan` says when it meets
it, and which ramp applies. Think of it as a field guide you can hand a
new teammate instead of a lore dump.

The tool's promise across every species, unchanged: **nothing is
clobbered, nothing is silently rewritten, and every dead end prints its
ramp.** The differences are only in how much you have to do before the
machine can help.

## Species 1: the sedimentary `AGENTS.md`

```text
project/
├── AGENTS.md            # 400 lines: 3 real laws, a linter grudge,
│                        # the coffee policy, someone's "always pnpm!!"
└── .codex-tmp/          # ...generated schemas that look like config
```

*Met in the wild: sleipniterm (Windows, pwsh), every codebase of a
certain agentic age.*

**Scan says:** `residual: AGENTS.md - unmanaged agent note file` plus
usually a `near-miss` about the lookalike directory.

**The move:** `harnessator kickstart .` drops
`HARNESSATOR-KICKSTART.md` - live findings, the mission (split by
concern, strip secrets *and list what you removed*, trivia to
`90-local-notes.md`), the gate commands. Or `harvest --with-notes` when
the headings genuinely map to concerns. Then publish plain `harvest` -
once `rules/` files exist, `--with-notes` would re-import the fossil
alongside your work (the first external agent run caught that one for
us, and the brief now warns about it).

**Renaming near-misses is a human commit:** if `.codex-tmp/` really is
agent content, `git mv` it to `.codex/`; if it is generated schemas,
say so once in a comment and let the near-miss note rest.

## Species 2: the scattered forest

```text
monorepo-or-home/
├── app-a/.claude/skills/deploy/SKILL.md
├── app-b/skills/deploy/SKILL.md          # same name, drifted content
├── app-c/rules/testing.md
└── tools/.opencode/command/fix-flaky.md
```

*Met in the wild: every team that let three projects bootstrap from
three tutorials.*

**Scan says:** components per root, and the money line at forest level:
`clusters ... DIVERGENT (2 variant(s))` - same component id, different
digests, surfaced *by content hash*, by digest, not by name.

**The move:** `harnessator harvest --recursive` (one governed source
out of the whole tree; `--prefer newest|largest` when divergence is
real, because the tool never picks a winner silently) then `--adopt` to
turn the donors into governed consumers - originals replaced by managed
links *only where the digest matches*; anything else stays, loudly.

## Species 3: the excellent repo that never asked to be a package

```text
someone-elses-project/
├── skills/
│   ├── brainstorming/SKILL.md
│   └── ...15 more
└── README.md                             # no harnessator.toml, ever
```

*Met in the wild: obra/superpowers (whose maintainer answered our
manifest PR with a reasoned "no, thanks" - and was right for their
project).*

Three answers, in increasing willingness to do the work yourself:

1. **A bridge** - you mirror, curate, and publish `aignerdev/…`-style
   snapshot repos with a pointer manifest. Costs: deploy keys, drift
   crons, and a real license question (mirroring *is* redistributing;
   Anthropic's terms and Composio's licensing refused us, and they were
   allowed to).
2. **A pointer source** - `[content] git + ref` in YOUR manifest,
   bytes fetched straight from THEIR repo, both commits pinned in the
   lock, `discover` evaluated against their tree. Nothing is copied,
   so pointing stays a recommendation, not a redistribution. Content is
   import-only *by construction*: `dev` refuses with the sanctioned
   moves, and badges say `(import-only)` everywhere.
3. **Inline consumer config** - for foreign content without any
   manifest at all, declare `editable = false` on the source entry and
   treat upstream as read-only by policy.

## Species 4: the same-name twins

```text
donors/
├── alpha/tools/     # harnessator.toml inside, perfectly valid
└── beta/tools/      # ...equally valid, entirely different repo
```

*Met in the wild: this project's own demos - the incident that produced
the identity guard.*

**The trap:** the store keys clones by source *name* (per user!), and a
name defaults to the repo basename. Two `tools/` donors, one `tools`
slot.

**Since v0.9.1:** the collision refuses with both ramps printed -
give it its own `--name`, or `harnessator source forget <name>` to hand
the name over. `check` finds stale aliasing offline, `doctor` too. The
lock records `(git, ref, manifest)` per source, so identity is always
the triple; the *name* is just the store's shelf label, and shelf
labels must be unique on the shelf.

## Species 5: the legacy single-file dialects

```text
project/
├── .cursorrules              # one flat file, pre-rules-era Cursor
├── .clinerules               # one flat file OR a directory - Cline
│                             # accepts both shapes
└── .github/
    └── copilot-instructions.md
```

**What harnessator sees today:** `.cursorrules` (and `AGENTS.md`,
`CLAUDE.md`, `GEMINI.md`, `QWEN.md`) at an unmanaged root are note-file
residuals - kickstart material. `.clinerules` as a *directory* is
recognized as rule components by the cline adapter's layout claims; as
a *single file* it is not yet flagged (honest gap, tracked below).
`.github/copilot-instructions.md` is content-bearing but lives a
directory deeper than the residual scan walks today - split it by hand
into `rules/` (it usually is one concern) or file the layout story.

**The move:** these files *become* rules via Species 1's ramps, then the
adapters distribute them natively (`.cursor/rules/*.mdc`,
`.github/instructions/*.instructions.md`, `.clinerules/*.md`) - the
originals stay until *you* retire them in a human commit.

## Species 6: the corporate registry (the shape we want more of)

```text
governance-repo/
├── corpus/                      # one source: rules/skills/policies
│   └── harnessator.toml
├── platform/                    # a second, independent source
│   └── harnessator.toml
└── providers/litellm.toml       # governance as ordinary components
```

One repo, many sources - the identity triple `(git, ref, manifest)`
makes each sub-directory a first-class source (`source add …
--manifest platform/harnessator`). Layering (corporate → team →
project → personal → local) plus a `harnessator.lock` per project is
what makes the whole thing reviewable: CI runs `check --json`, the PR
diff shows a lock delta, and the humans approve exactly which governed
bytes moved. Hired a new laptop? `harnessator setup` rebuilds every
adapter tree from the lock, offline if the store is warm.

## Species 7: the fossil that is *also* the pointer (and other loops)

```text
consumer-project/
├── harnessator.toml + harnessator.lock      # managed root
└── AGENTS.md, .harnessator/active.md, ...   # generated pointers
```

A managed root's `AGENTS.md` is *harnessator's own output* (or a
redirect we leave alone), not import material - `findNoteFiles` and the
residual scan deliberately skip roots holding a `harnessator.lock`,
because harvesting your own pointer files into a source would be a
very elegant ouroboros. If you `git clone` an old managed project into
a fresh unmanaged directory, though, the generated notes become a
Species 5 fossil - and the tool can no longer tell it was born from a
template. Review the import like any other.

## The slot matrix (one screen, nine targets)

| category | opencode | claude | codex | antigravity | pi | cursor | gemini | copilot | cline |
|---|---|---|---|---|---|---|---|---|---|
| rule | active index + native | active index¹ | active index¹ | active index¹ | active index¹ | `.cursor/rules/*.mdc` (generated) | `GEMINI.md` block | `.github/instructions/*.instructions.md` | `.clinerules/*.md` (links) |
| skill | `.opencode/skill/` | `.claude/skills/` | `.agents/skills/` | `.agents/skills/` | farm → `.agents/skills/` | `.agents/skills/` | `.agents/skills/` | `.agents/skills/` | `.agents/skills/` |
| prompt/workflow | `.opencode/command/` | `.claude/commands/` | `.codex/prompts/` | `.agents/workflows/` | — | `.cursor/commands/` | `.gemini/commands/*.toml` (generated) | `.github/prompts/*.prompt.md` | `.clinerules/workflows/*.md` |
| agent | `.opencode/agent/` | `.claude/agents/` | — | — | — | — | `.gemini/agents/` | `.github/agents/*.agent.md` | `.cline/agents/` |
| mcp | `opencode.json` | `.mcp.json` + `settings.local.json` | `config.toml` block | `.agents/mcp_config.json` | — | `.cursor/mcp.json` | `.gemini/settings.json` (only if ours²) | `.vscode/mcp.json` + `.mcp.json` | `.cline/mcp.json` |
| hook | `.opencode/plugin/` | `settings.local.json` | — | — | package farm | — | — | — | — |
| tool | `.opencode/tool/` | — | — | — | package farm | — | — | — | — |

¹ the active index in `.harnessator/active.md` is what AGENTS-style
harnesses load; rules live in the store and are referenced, never
copied, unless the harness needs native format.
² when the human's `settings.json` already exists, harnessator reports
a loud skip instead of touching model config that isn't its business.

Anything a target has no slot for is a SKIP line with a reason -
greppable, visible in `check`, never a silent drop.

## Run the bestiary

One block, three species coexisting, output exactly as observed:

```sh
mkdir -p zoo/skills/brainstorming
printf '# rules\n\n## test first\nalways\n\n## commit scoped\none concern\n' > zoo/AGENTS.md
printf 'always use tabs, never argue about tabs\n' > zoo/.cursorrules
printf -- '---\ndescription: Brainstorm before building.\n---\nsteps\n' > zoo/skills/brainstorming/SKILL.md
harnessator scan zoo
harnessator harvest --dry-run zoo --name zoo-tools
harnessator harvest --dry-run zoo --name zoo-tools --with-notes
```

```text
zoo  [unmanaged]  1 component(s), ..., 2 residual, 0 near-miss
  skill    brainstorming   sha256:c1bb620a21...
  residual: .cursorrules - unmanaged agent note file - ...
  residual: AGENTS.md - unmanaged agent note file - ...

# plain dry-run: publishes only what is already well-shaped...
  add  skill  brainstorming   .../zoo/skills/brainstorming -> skills/brainstorming

# ...and --with-notes adds the heading-decoded fossil beside it:
  add  rule  commit-scoped    .../zoo/AGENTS.md § commit scoped -> rules/commit-scoped.md
  add  rule  cursorrules-md   .../zoo/.cursorrules -> rules/cursorrules-md.md
  add  rule  test-first       .../zoo/AGENTS.md § test first -> rules/test-first.md
```

Three lessons from six lines of output: layout (the skill) publishes
itself; structure (the headings) decodes itself; and the flat legacy
file imports as one honest verbatim rule - the tool guessing nothing
about content it cannot see. Writing this page caught a bug in that
last path, too: the dotfile's stem vanished into an id of `md` - now
fixed (`.cursorrules` yields `cursorrules-md`), found precisely because
the guide shows real output, not imagined output.

## Honest gaps (this page self-documents its own tool)

- **Residuals live at scan roots only.** A nested `AGENTS.md` in a
  sub-folder that is not itself a project root stays invisible until
  you scan deeper - which also means single-file `.clinerules` and
  `.github/copilot-instructions.md` (not in `rootNoteFiles`, or one
  directory deep) are content the scan walks past today. Honest
  ramp: add them to the roots you scan, or split by hand; candidate
  `rootNoteFiles`/depth additions for a code round that can test them
  properly.
- **`harvest --commit` provenance ordering** - the sleipniterm agent
  had to correct an origin SHA recorded before the donor commit
  existed; open investigation.
- **Governance dialects** for gemini/copilot/cline: adapters ship,
  provider rendering is a loud skip until each harness's model-config
  surface is verified against docs (pi/cursor/antigravity carry the
  same open line since the MVP).

## Related pages

- [legacy-notes-import](../dev/legacy-notes-import.md) - Species 1 in full motion
- [adopt-existing-projects](../dev/adopt-existing-projects.md) - Species 2 end to end
- [pointer-sources](pointer-sources.md) - Species 3's best answer
- [corporate-registry](corporate-registry.md) - Species 6 as a platform blueprint
- [ci-and-review](ci-and-review.md) - how Species 6 stays honest in PRs
