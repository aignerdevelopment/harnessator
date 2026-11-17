# Model governance for platform teams

The feature buyers actually ask about: "which model does this tool use,
who decided, and can a developer silently change it?" This guide is the
enforcement side of the gateway story - the developer side is
[configure-gateway-access](../dev/configure-gateway-access.md).

## The two component types

**Provider** (`providers/*.toml` in a source): the gateway endpoint +
the approved catalog. Endpoints are keyed by **API family**, not by
tool, so one LiteLLM deployment serves Claude Code, opencode and Codex
natively from one file:

```toml
name          = "litellm-corp"
auth_env      = "LITELLM_API_KEY"          # NAME only - values live in env
default_model = "big"

[styles.openai-compatible]                 # for opencode / codex
base_url = "https://litellm.acme.internal/v1"
[styles.anthropic-compatible]              # for Claude Code
base_url = "https://litellm.acme.internal/anthropic"

[models]                                   # the approved catalog
big   = "claude-sonnet-5"                  # aliases are what teams select
fast  = "claude-haiku-5.5"
smart = "gpt-5.2"
```

**Policy** (`policies/*.toml`): what's allowed from that catalog.

```toml
reason         = "approved for code generation"
strict         = true
allowed        = ["big", "smart"]
subagent_model = "big"
```

## What setup enforces, mechanically

- `strict` (default): selections outside `allowed` **block setup** -
  not a warning that someone learns to ignore, an error that fails CI.
- Every harness rendering is narrowed to the allowed set: the opencode
  picker *cannot offer* a denied model; Claude Code gets the endpoint
  plus tier env; Codex gets the provider stanza.
- **Sub-agent pinning**: `CLAUDE_CODE_SUBAGENT_MODEL` kills the classic
  hole where "sub-agents run on whatever" silently escapes the
  catalog. This is the detail that makes Claude Code governance real.
- Layered relaxation only: a project may widen only via an explicit
  higher-layer policy component - the `why` trail shows which layer
  relaxed what.
- Rotation is free: only env var **names** are committed, so the
  gateway key rotates without touching a single repo.

## Run the enforcement (live, in two minutes)

Create a source with a catalog that forbids its own default model, and
watch the gate close:

```sh
harnessator source new --name acme-models --dir acme-models
mkdir -p acme-models/providers acme-models/policies
cat > acme-models/providers/gw.toml <<'EOF'
name = "acme-gw"
auth_env = "ACME_LLM_KEY"
default_model = "big"
[styles.openai-compatible]
base_url = "https://gw.acme.internal/v1"
[models]
big = "model-big"
smart = "model-smart"
EOF
cat > acme-models/policies/deny-default.toml <<'EOF'
reason = "demo: forgot to allow the default"
strict = true
allowed = ["smart"]
EOF
cat >> acme-models/harnessator.toml <<'EOF'

[component.provider."gw"]
path = "providers/gw.toml"
description = "Acme gateway catalog."

[component.policy."deny-default"]
path = "policies/deny-default.toml"
description = "Strict allow-list (demo) missing the default."
EOF
git -C acme-models init -qb main
git -C acme-models add -A
git -C acme-models -c user.name=gov -c user.email=gov@example.com commit -qm "models source"
```

Now a consumer project selects provider + policy:

```sh
SRC=$PWD/acme-models
mkdir -p app && cd app
harnessator init --targets claude --source "file://$SRC" .
harnessator enable acme-models:provider/gw
harnessator provider use acme-models:provider/gw
harnessator enable acme-models:policy/deny-default
harnessator setup || true
```

Setup **fails the project** with a model policy violation: the selected
catalog's `default_model = "big"` is not in `allowed` - exactly the
mistake a weaker tool would ship silently. The fix is a source-side
change, reviewed and pinned like any other:

```sh
cd ..
sed -i 's/allowed = \["smart"\]/allowed = ["smart", "big"]/' acme-models/policies/deny-default.toml
cd acme-models
git add -A && git -c user.name=gov -c user.email=gov@example.com commit -qm "policy: allow the catalog default"
cd ../app
harnessator update
harnessator setup
harnessator check --json
```

The app config now renders catalog-narrowed files for its harnesses,
`check` passes, and the lock proves which catalog revision the team
reviewed.

## Honest limits

- This is a **client-side gate**: strong guardrail, auditable by
  construction, bypassable by someone who edits config by hand and
  removes the pin - which is why `verify`/`check` belong in CI, and why
  the gateway's own auth model (see the security review) is the real
  last line.
- Cursor's model control is server-side (Team/Enterprise plans);
  harnessator reports that and configures what it can.
- Targets whose endpoint file shape is unverified (pi) skip governance
  loudly - never silently.
