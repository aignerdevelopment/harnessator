# Guides

Task-shaped journeys, written to be **executed as written**. USAGE.md
lists every flag; these pages answer "how do I get from where I am to a
governed setup".

Every command block is runnable - the guides are verified on clean
machines before release, and CI fails if any block references a command
or flag that does not exist.

## I am a developer…

| I want to… | Go to |
|---|---|
| find my path in one read | [JOURNEYS](JOURNEYS.md) - four personas, four walks through the guides below |
| start a brand-new project, governed from minute one | [quickstart-new-project](dev/quickstart-new-project.md) |
| get my *existing* scattered skills/rules under control | [adopt-existing-projects](dev/adopt-existing-projects.md) |
| use my company's model gateway (LiteLLM & friends) | [configure-gateway-access](dev/configure-gateway-access.md) |
| know what my Tuesday looks like (updates, edits, drift) | [day-in-the-life](dev/day-in-the-life.md) |
| try every AI coding harness without losing my setup | [day-in-the-life-tool-hopper](dev/day-in-the-life-tool-hopper.md) |
| move a project from one harness to another | [switch-harness](dev/switch-harness.md) |

## I run the platform team…

| I want to… | Go to |
|---|---|
| build the company's component registry (layers, review, CI) | [corporate-registry](platform/corporate-registry.md) |
| decide which models everyone may use, and enforce it | [model-governance](platform/model-governance.md) |
| wire the enforcement gate into pipelines | [ci-and-review](platform/ci-and-review.md) |
| know what this actually protects against (and what not) | [security-review](platform/security-review.md) |

## The long version

Everyday questions with one-paragraph answers: [cookbook](cookbook.md).

## Conventions

- `harnessator` is assumed to be on your `PATH` - install or update it
  in one line (sha256-verified):
  `curl -fsSL https://raw.githubusercontent.com/aignerdevelopment/harnessator/main/install.sh | bash`
  (Windows: `irm .../install.ps1 | iex`).
- Examples use the public reference corpus
  [`aignerdevelopment/agentictools`](https://github.com/aignerdevelopment/agentictools)
  as the source - real components you can run against today.
- Blocks marked `sh` are commands; `toml`/`text` blocks are file contents.
- Flags and full reference: [USAGE.md](../../USAGE.md) and the man pages.
