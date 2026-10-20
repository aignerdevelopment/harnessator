#!/usr/bin/env bash
# scripts/run-guides.sh - execute every ```sh code block in
# docs/guides/{dev,platform}/*.md inside a pristine sandbox: a fresh
# HARNESSATOR_HOME store, a per-guide temp project directory, the
# harnessator binary under test first on PATH, and a synthetic git
# identity so the guides' `git init/commit` blocks run unattended.
#
# This is the acceptance criterion of the guide set ("runs as written")
# - run it in CI before releases, not after bug reports.
set -euo pipefail
REPO=$(git rev-parse --show-toplevel)
BIN=$(mktemp -d)
go build -o "$BIN/harnessator" "$REPO/cmd/harnessator"

python3 - "$REPO" "$BIN" <<'PY'
import os, re, subprocess, sys, tempfile

repo, bindir = sys.argv[1], sys.argv[2]
guides = []
for area in ("dev", "platform"):
    d = os.path.join(repo, "docs/guides", area)
    guides += [os.path.join(d, f) for f in sorted(os.listdir(d)) if f.endswith(".md")]

fail = False
for g in guides:
    text = open(g).read()
    blocks = re.findall(r"```sh\n(.*?)```", text, re.S)
    if not blocks:
        print(f"SKIP {os.path.relpath(g, repo)} (no sh blocks)")
        continue
    env = dict(os.environ)
    env.update({
        "HARNESSATOR_HOME": tempfile.mkdtemp(),
        "PATH": bindir + os.pathsep + env["PATH"],
        "GIT_AUTHOR_NAME": "guidebot", "GIT_AUTHOR_EMAIL": "guide@example.com",
        "GIT_COMMITTER_NAME": "guidebot", "GIT_COMMITTER_EMAIL": "guide@example.com",
        "GIT_TERMINAL_PROMPT": "0",
    })
    work = tempfile.mkdtemp()
    script = "set -e\n" + "\n".join(f"echo '>>> block {i+1}/{len(blocks)}'\n" + b
                                     for i, b in enumerate(blocks))
    r = subprocess.run(["bash", "-c", script], cwd=work, env=env,
                       capture_output=True, text=True)
    tag = "OK  " if r.returncode == 0 else "FAIL"
    print(f"{tag} {os.path.relpath(g, repo)} ({len(blocks)} blocks)")
    if r.returncode:
        fail = True
        print(r.stdout[-1500:])
        print("STDERR:", r.stderr[-2500:], file=sys.stderr)
sys.exit(1 if fail else 0)
PY
