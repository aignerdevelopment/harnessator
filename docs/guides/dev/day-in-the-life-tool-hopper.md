# A Day in the Life (II): The Tool-Hopper

The first [day-in-the-life](day-in-the-life.md) was about stability:
one harness, reviewable updates, drift that fails CI. This one is about
the opposite person: you want to try *everything* - Claude in the
morning, opencode after lunch, Codex on a bet, and whatever launches on
Friday - without your agent knowledge turning into per-tool folklore.

The premise in one sentence: **harness content is pinned once; harness
*renderings* are a config line wide.**

## Morning: Claude, from zero

```sh
git init -b main
harnessator init --targets claude --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:minimal
harnessator setup
ls .claude
```

The store now holds the pinned content; `.claude/` holds symlinks and
generated blocks. No harness was installed to make this happen -
harnessator materializes *configuration*, and it does so honestly:
anything a target cannot take is reported, never silently dropped.

## Afternoon: "also show me opencode and codex"

`targets` is declarative config - widening it changes what gets
rendered, not what you selected:

```sh
sed -i 's/targets = \["claude"\]/targets = ["claude", "opencode", "codex"]/' harnessator.toml
harnessator setup
ls -d .claude .opencode .codex
harnessator list
harnessator check --json
```

Three harness folders, one plan, one lock - `list` output is
byte-identical for all of them because the *content* did not change;
only the renderings grew. Restart whichever harness you actually have
installed; the folders for the ones you don't just... wait politely.

## Late afternoon: the task toolchain

You are prototyping UI in one repo and firmware in another, same
corporate sources, different daily toolchain. This is the local layer -
per-project, personal, gitignored:

```sh
harnessator enable agentictools:skill/example-skill --layer local
harnessator setup
harnessator layers
harnessator why skill/example-skill
```

`layers` shows the merge (project sources, your local picks); `why` says
`[via local]` - the other you, three weeks from now, will thank you.

## Evening: let the tool inspect itself

Whichever agent is open, it can ask its own configuration questions -
read-only, via MCP:

```console
$ harnessator mcp serve
# (agent-side, through the MCP connection)
#   list_active   -> what this project enables, with provenance
#   why           -> who made it active, from which layer file
#   search        -> what else the pinned sources offer
#   policy_status -> the model catalog in effect
```

Wire it once per harness (`harnessator enable harnessator:mcp/harnessator`
in projects that use the published corpus as a source) and every tool
you try tonight answers from the same truth.

## Friday: leaving a tool behind

```console
$ # remove "codex" from targets in harnessator.toml, then:
$ harnessator setup
$ harnessator doctor        # names anything stale
```

Generated folders are yours to delete; managed content outside them was
never touched. The pins, provenance and policy stay - the harness was
always the costume, not the actor.

## What this day proves

- Trying a harness costs one line + one `harnessator setup`.
- Trying *every* harness does not fork your knowledge base, because the
  content was never per-harness - it is pinned, layered, provenance-tagged.
- The escape hatch is not a migration plan; it is `targets`.
