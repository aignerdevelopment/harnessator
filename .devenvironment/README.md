# .devenvironment

Single place for every environment-dependent value: git URL and credentials,
service URLs, MCP URLs and credentials, and anything else that varies per
environment.

## Files

| File | Committed | Purpose |
| --- | --- | --- |
| `env.sh` | yes | Defines all keys with non-secret defaults / placeholders and sources `env.local.sh`. |
| `env.local.sh` | **no** (gitignored) | Real, machine-specific values. Never commit it. |

## Usage

From the repository root, before compiling/building, running tests, setting up
the environment, or invoking any configured/authenticated tool or MCP server:

```sh
. ./.devenvironment/env.sh
```

Then use the exported variables. Never hardcode URLs, hostnames, tokens or
credentials in code, scripts, tool definitions or MCP config — reference the
environment variable instead.

Check which keys still need filling:

```sh
. ./.devenvironment/env.sh
devenv_check
```

## Placeholder convention

Every key that needs a value is written as:

```
<insert your .... here>
```

When you add a tool, MCP server or service that needs configuration or
authentication, add the matching key to `env.sh` with that placeholder, then
reference the variable from the tool/MCP/service config.

See rule `07-devenvironment`.
