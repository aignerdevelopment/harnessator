# Switching harnesses

Teams change tools - Claude Code to opencode, adding Codex, piloting
Antigravity. The governed content does **not** change; only its
renderings. This guide shows the switch and what to watch for.

## Current state: a Claude Code project

```sh
mkdir move && cd move
git init -b main
harnessator init --targets claude \
  --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:minimal
harnessator setup
ls .claude
```

Components are symlinked into the pinned store; skills, agents and
hooks sit in the layout Claude Code natively reads, and shared config
is spliced into `settings.local.json` / `.mcp.json`.

## Team switches to opencode

The switch is one line in `harnessator.toml` - `targets` is declarative
config, not tool state:

```sh
sed -i 's/targets = \["claude"\]/targets = ["opencode"]/' harnessator.toml
harnessator setup
ls .opencode
```

Same components, now rendered into `.opencode/` (skills, agents,
commands, plus a generated `opencode.json` with the instructions
redirect and MCP servers). The old `.claude/` directory is stale
rendering, not state - clean it, and `doctor` will have told you it
exists:

```sh
rm -rf .claude
harnessator doctor
harnessator list
```

`list`/`check` are unchanged: the plan and the lock don't care which
harnesses consume them.

## Adding a second harness while keeping the first

Just widen the list:

```sh
sed -i 's/targets = \["opencode"\]/targets = ["opencode", "claude"]/' harnessator.toml
harnessator setup
```

Now both harness directories are maintained from one source - the
answer to "each IDE wants its own folder" is that they each get *their
folder*, all pointing at the same pinned truth.

## What does not magically switch

Per-target capability differences are surfaced, never hidden - re-read
`setup` output with `harnessator list --all`:

- **mcp/tools**: every MVP target renders them (native or generated
  config block).
- **governance (provider/policy)**: renders for opencode/claude/codex;
  pi/cursor/antigravity report a loud skip - their endpoint control is
  unverified or server-side, and harnessator says which.
- **hooks**: native for Claude Code; elsewhere they render as the
  closest adapter or a reported skip.

A component that cannot go somewhere is **reported**, not dropped - so
switching harnesses never quietly loses policy.

## Related

- [configure-gateway-access](configure-gateway-access.md) - the per-harness governance renderings
- [switching from a hand-synced setup](adopt-existing-projects.md) - when the old harness config was copy-paste
