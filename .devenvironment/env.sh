#!/usr/bin/env sh
# .devenvironment/env.sh
#
# Central definition of every environment-dependent value for this project:
# git URL and credentials, service URLs, MCP URLs and credentials, and anything
# else that changes between environments.
#
# Usage — from the repository root, BEFORE compiling, building, testing,
# setting up the environment, or invoking any configured/authenticated tool or
# MCP server:
#
#     . ./.devenvironment/env.sh
#
# This file is committed and must contain NO secrets: only non-secret defaults
# and placeholders. Real values go in the gitignored sibling `env.local.sh`,
# which is sourced automatically at the end.
#
# Placeholder convention (rule 07-devenvironment): every key that needs a value
# is written as
#
#     <insert your .... here>
#
# When you add a tool, MCP server or service that needs configuring or
# authenticating, add the corresponding key below with that placeholder and
# reference the variable from the tool/MCP/service config (never hardcode).

# ---------------------------------------------------------------------------
# git
# ---------------------------------------------------------------------------
[ -n "${GIT_URL:-}" ] || GIT_URL="<insert your git url here>"
[ -n "${GIT_USERNAME:-}" ] || GIT_USERNAME="<insert your git username here>"
[ -n "${GIT_PASSWORD:-}" ] || GIT_PASSWORD="<insert your git password or token here>"
export GIT_URL GIT_USERNAME GIT_PASSWORD

# ---------------------------------------------------------------------------
# services
# ---------------------------------------------------------------------------
[ -n "${SERVICE_API_URL:-}" ] || SERVICE_API_URL="<insert your service api url here>"
[ -n "${SERVICE_API_TOKEN:-}" ] || SERVICE_API_TOKEN="<insert your service api token here>"
export SERVICE_API_URL SERVICE_API_TOKEN

# ---------------------------------------------------------------------------
# MCP
# ---------------------------------------------------------------------------
[ -n "${MCP_BASE_URL:-}" ] || MCP_BASE_URL="<insert your mcp base url here>"
[ -n "${MCP_API_TOKEN:-}" ] || MCP_API_TOKEN="<insert your mcp api token here>"
export MCP_BASE_URL MCP_API_TOKEN

# ---------------------------------------------------------------------------
# gitlab (used by the gitlab-tool / issue workflow)
# ---------------------------------------------------------------------------
[ -n "${GITLAB_TOKEN:-}" ] || GITLAB_TOKEN="<insert your gitlab token here>"
[ -n "${GITLAB_BASE_URL:-}" ] || GITLAB_BASE_URL="<insert your gitlab base url here>"
[ -n "${GITLAB_PROJECT_PATH:-}" ] || GITLAB_PROJECT_PATH="<insert your gitlab project path here>"
[ -n "${GITLAB_PROJECT_ID:-}" ] || GITLAB_PROJECT_ID=""
[ -n "${GITLAB_USERNAME:-}" ] || GITLAB_USERNAME="<insert your gitlab username here>"
[ -n "${GITLAB_DIR:-}" ] || GITLAB_DIR="GITLAB"
export GITLAB_TOKEN GITLAB_BASE_URL GITLAB_PROJECT_PATH GITLAB_PROJECT_ID GITLAB_USERNAME GITLAB_DIR

# ---------------------------------------------------------------------------
# jira (used by the jira-issue tool / skill)
# ---------------------------------------------------------------------------
[ -n "${JIRA_BASE_URL:-}" ] || JIRA_BASE_URL="<insert your jira base url here>"
[ -n "${JIRA_EMAIL:-}" ] || JIRA_EMAIL="<insert your jira email here>"
[ -n "${JIRA_API_TOKEN:-}" ] || JIRA_API_TOKEN="<insert your jira api token here>"
[ -n "${JIRA_DIR:-}" ] || JIRA_DIR="JIRA"
export JIRA_BASE_URL JIRA_EMAIL JIRA_API_TOKEN JIRA_DIR

# ---------------------------------------------------------------------------
# project documentation
# ---------------------------------------------------------------------------
[ -n "${PROJECT_DOCS_DIR:-}" ] || PROJECT_DOCS_DIR="docs/project"
export PROJECT_DOCS_DIR

# ---------------------------------------------------------------------------
# local, gitignored overrides (real values live here)
# ---------------------------------------------------------------------------
_devenv_self="${BASH_SOURCE:-$0}"
_devenv_dir="$(cd "$(dirname "$_devenv_self")" 2>/dev/null && pwd || pwd)"
if [ -f "$_devenv_dir/env.local.sh" ]; then
  . "$_devenv_dir/env.local.sh"
elif [ -f ./.devenvironment/env.local.sh ]; then
  . ./.devenvironment/env.local.sh
fi
unset _devenv_self _devenv_dir

# Report keys that are still placeholders / empty.
# Usage: devenv_check   (returns non-zero if anything is unconfigured)
devenv_check() {
  _devenv_missing=0
  for _devenv_var in \
    GIT_URL GIT_USERNAME GIT_PASSWORD \
    SERVICE_API_URL SERVICE_API_TOKEN \
    MCP_BASE_URL MCP_API_TOKEN \
    GITLAB_TOKEN GITLAB_BASE_URL \
    JIRA_BASE_URL JIRA_EMAIL JIRA_API_TOKEN
  do
    eval "_devenv_val=\${$_devenv_var:-}"
    case "$_devenv_val" in
      ''|*"<insert your"*)
        printf '%s\n' "devenvironment: $_devenv_var is not configured" >&2
        _devenv_missing=1
        ;;
    esac
  done
  unset _devenv_var _devenv_val
  return $_devenv_missing
}
