# Configure gateway access (developer view)

Your platform team runs a model gateway - typically LiteLLM Proxy, which
is quickly *the* model entry point in a corporate environment: it sits
in front of Anthropic/OpenAI/self-hosted backends, handles keys, budgets
and logs. This guide is the consuming side: point your harnesses at the
gateway, get the approved model list, and never paste a token into a
config file.

If you are the one *running* the gateway and deciding the catalog, read
[model-governance](../platform/model-governance.md) instead - it is the
same machinery from the other side of the table.

## 0. What the platform published

Governance arrives as two ordinary component types from a source repo:

```toml
# a provider profile - endpoints + the approved catalog
name          = "litellm-corp"
default_model = "big"
auth_env      = "LITELLM_API_KEY"

[styles.openai-compatible]
base_url = "https://litellm.acme.internal/v1"

[styles.anthropic-compatible]
base_url = "https://litellm.acme.internal/anthropic"

[models]
big   = "claude-sonnet-5"
fast  = "claude-haiku-5.5"
smart = "gpt-5.2"
```

and a policy component (usually `allowed = [...]` + strict). The
public reference corpus ships exactly this pair as runnable examples -
we use them below; swap in your platform's source name and ids.

## 1. Select provider + policy

```sh
mkdir gw && cd gw
harnessator init --targets claude,opencode \
  --source https://github.com/aignerdevelopment/agentictools.git .
harnessator enable agentictools:provider/litellm-demo
harnessator provider use agentictools:provider/litellm-demo
harnessator enable agentictools:policy/models-strict
harnessator setup
```

`provider use` pins which profile the project runs on; the policy then
narrows every model the harness may offer. One command shows the
result:

```sh
harnessator provider show
```

## 2. What actually changed on disk

Same canonical file, rendered natively per harness - you never edit
these directly:

| harness | what setup writes |
|---|---|
| **opencode** | `opencode.json` provider block: `@ai-sdk/openai-compatible` npm pkg, `baseURL` to the gateway, `apiKey: "{env:LITELLM_API_KEY}"`, and a **model list narrowed to the allowed set** - the picker itself cannot offer you a forbidden model |
| **Claude Code** | `settings.local.json` env: `ANTHROPIC_BASE_URL` to the `/anthropic` path, `ANTHROPIC_MODEL` default, tier aliases (`ANTHROPIC_DEFAULT_SONNET_MODEL` …) and `CLAUDE_CODE_SUBAGENT_MODEL` - sub-agents stay on approved models too |
| **codex** | managed `[model_providers.litellm-corp]` stanza: `base_url`, `env_key`, `wire_api` |

Targets without governance support (pi, cursor, antigravity) say so
loudly in `setup` output instead of silently dropping the config.

## 3. The key

harnessator only ever writes env var **names** into configs - the value
stays outside git. On your workstation:

```sh
export LITELLM_API_KEY="***"        # or put it in .devenvironment/env.local.sh
```

Restart the harness; `harnessator check --json` passes and your model
list reflects the platform catalog. If the SSO/bot token rotates, nobody
commits anything - rotate the environment value.

## 4. The moment you try an unapproved model

Policies are strict by default. If you point a tool at a model outside
the allow-list, setup refuses with the catalog in the message. That is
the feature - the fix is not a config hack but a request to the platform
team, tracked as a reviewable change to the policy component.

## Related

- [model-governance](../platform/model-governance.md) - running the catalog side
- [day-in-the-life](day-in-the-life.md) - how the pin updates land
