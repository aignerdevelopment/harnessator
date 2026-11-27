#!/usr/bin/env python3
# Copyright 2026 Roland Aigner (https://www.aignerdevelopment.com)
# SPDX-License-Identifier: Apache-2.0
"""
bridge-sync - refresh a pinned harnessator bridge repo from its upstream.

The bridges (github.com/aignerdevelopment/{superpowers,cursor-rules,
copilot-skills}) are published sources: consumers pin their git tags.
This tool keeps them current WITHOUT ever moving a tag by itself -
tagging needs the explicit --new-tag input, consumers move by
'harnessator update', never by surprise.

Two manifest forms, detected automatically:
  pointer  ([discover], schema 2): the manifest registers content live,
           so a refresh only syncs files and records provenance. The
           manifest cannot drift - that is the answer to the objection
           that closed the upstream manifest PRs.
  explicit (harvested component list): refresh runs
           'harnessator harvest --into', which merges new components
           (fresh origin + license carry) and idempotently skips
           identical ones. Conflicts, sanitize findings or removed
           components make harvest refuse - the tool reports "human
           review needed" and pushes nothing. Silent partials are the
           failure mode this exists to prevent.

Every action goes through harnessator's own validate --check-paths as
the last gate before any push. Pushes need a deploy key (--key) with
write access on the bridge repo; --dry performs everything except
push/tag. stdlib only.
"""

import argparse, json, os, re, shutil, subprocess, sys, tempfile, tomllib
from datetime import date


def run(args, cwd=None, env=None, check=True):
    r = subprocess.run(args, cwd=cwd, env=env, capture_output=True, text=True)
    if check and r.returncode != 0:
        sys.exit(f"FATAL: {' '.join(args)}\n{r.stdout}\n{r.stderr}")
    return r


def git_env(key_path):
    env = dict(os.environ)
    if key_path:
        env["GIT_SSH_COMMAND"] = (
            f"ssh -i {key_path} -o IdentitiesOnly=yes "
            "-o StrictHostKeyChecking=accept-new")
    return env


def latest_ref(upstream, want):
    if want:
        return want
    r = run(["git", "ls-remote", "--tags", "--sort=-v:refname", upstream],
            check=False)
    for line in (r.stdout or "").splitlines():
        sha, ref = line.split("\t", 1)
        if ref.startswith("refs/tags/v") and not ref.endswith("^{}"):
            return ref[len("refs/tags/"):]
    r = run(["git", "ls-remote", "--symref", upstream, "HEAD"], check=False)
    m = re.search(r"ref: refs/heads/(\S+)", r.stdout or "")
    if not m:
        sys.exit(f"FATAL: cannot resolve a default ref at {upstream}")
    return m.group(1)


def scan_hits(bin_, content_dir):
    r = run([bin_, "scan", "--json", content_dir], check=False)
    if r.returncode != 0:
        sys.exit(f"FATAL: scan of synced content failed:\n{r.stderr}")
    hits = []
    rep = json.loads(r.stdout)
    for root in rep.get("roots", []):
        for c in root.get("components", []):
            for h in c.get("sanitizeHits", []) or []:
                hits.append(f"  {h['rule']} at {h['file']}:{h['line']} {h['sample']}")
    return hits


def sync_pointer(bridge_dir, up_dir, cfg):
    dst = os.path.join(bridge_dir, cfg["content"])
    src = os.path.join(up_dir, cfg["root"])
    if not os.path.isdir(src):
        sys.exit(f"FATAL: upstream root {cfg['root']!r} not found in {up_dir}")
    shutil.rmtree(dst, ignore_errors=True)
    shutil.copytree(src, dst)
    # the pointer form skips harvest - so the sanitize gate must run here,
    # or a fresh upstream secret would sail straight into the bridge.
    hits = scan_hits(os.environ.get("HARNESSATOR_BIN", "harnessator"), dst)
    if hits:
        msg = (f"{len(hits)} sanitize finding(s):\n" + "\n".join(hits[:20]))
        if not cfg.get("allow_hits"):
            print("BLOCKED: " + msg + "\nno pre-review on record for this "
                  "bridge - a human must review before it can flow.")
            sys.exit(2)
        print(f"NOTE: {msg} - pre-reviewed, see HARVEST-NOTES.md "
              "in this bridge (allow_hits recorded in bridges.json)")


def sync_explicit(bridge_dir, up_dir, cfg, bin_env):
    root = os.path.join(up_dir, cfg["root"])
    cmd = [os.environ.get("HARNESSATOR_BIN", "harnessator"),
           "harvest", "--into", bridge_dir, root]
    if cfg.get("allow_hits"):
        cmd.insert(2, "--allow-hits")  # after the subcommand, not before it
    r = run(cmd, env=bin_env, check=False)
    if r.returncode != 0:
        print("BLOCKED: harvest refused - a human must review before this "
              "bridge moves:\n" + (r.stdout or "") + (r.stderr or ""))
        sys.exit(2)
    return r.stdout


def write_upstream_md(bridge_dir, cfg, ref, sha, mode):
    manifest = os.path.join(bridge_dir, "harnessator.toml")
    t = open(manifest, encoding="utf-8").read()
    if re.match(r"^v?\d", ref):  # versiony refs bump package.version only
        ver = re.sub(r"^v", "", ref)
        t = re.sub(r'(?m)^(  version = )".*"$', rf'\1"{ver}"', t)
    open(manifest, "w", encoding="utf-8").write(t)
    form = ("live discovery pointer (schema 2) - registration cannot drift; "
            "curation lives in the profiles only" if mode == "pointer" else
            "harvested component manifest (schema 1) - new components are "
            "merged by 'harnessator harvest --into', never hand-edited")
    open(os.path.join(bridge_dir, "UPSTREAM.md"), "w",
         encoding="utf-8").write(
        "# Upstream\n\n"
        f"- repository: {cfg['upstream'].removesuffix('.git')} (authors keep "
        "their license - see LICENSE in this tree, verbatim)\n"
        f"- ref: `{ref}`, commit `{sha}`\n"
        f"- synced: {date.today().isoformat()} by bridge-sync.py "
        "(harnessator repo, 'Bridge refresh' workflow)\n"
        f"- manifest form: {form}\n"
        "- refresh policy: content syncs on demand; **consumers move only "
        "when a new bridge tag is cut** ('harnessator update', never "
        "surprise)\n")


def main():
    a = argparse.ArgumentParser()
    a.add_argument("--bridge", required=True)
    a.add_argument("--config", default=os.path.join(
        os.path.dirname(__file__), "bridges.json"))
    a.add_argument("--work", default="")
    a.add_argument("--upstream-ref", default="")
    a.add_argument("--new-tag", default="")
    a.add_argument("--key", default="")
    a.add_argument("--dry", action="store_true")
    args = a.parse_args()

    key = args.bridge.upper().replace("-", "_").replace(" ", "_")
    all_cfgs = json.load(open(args.config))
    if key not in all_cfgs:
        sys.exit(f"FATAL: unknown bridge {args.bridge!r}; known: "
                 + ", ".join(sorted(all_cfgs)))
    cfg = all_cfgs[key]
    work = args.work or tempfile.mkdtemp(prefix=f"bridge-{args.bridge}-")
    os.makedirs(work, exist_ok=True)
    bridge_dir = os.path.join(work, "bridge")
    up_dir = os.path.join(work, "upstream")
    env = git_env(args.key)

    if os.path.isdir(bridge_dir):
        shutil.rmtree(bridge_dir)
    run(["git", "clone", "--", cfg["bridge"], bridge_dir], env=env)

    ref = latest_ref(cfg["upstream"], args.upstream_ref)
    if os.path.isdir(up_dir):
        shutil.rmtree(up_dir)
    run(["git", "clone", "--depth", "1", "--branch", ref, "--",
         cfg["upstream"], up_dir])
    sha = run(["git", "rev-parse", "HEAD"], cwd=up_dir).stdout.strip()

    man_path = os.path.join(bridge_dir, "harnessator.toml")
    man = tomllib.load(open(man_path, "rb"))
    mode = "pointer" if man.get("discover") else "explicit"
    print(f"bridge {args.bridge}: manifest form = {mode}; "
          f"upstream ref = {ref} ({sha[:12]})")

    bin_env = dict(os.environ)
    if mode == "pointer":
        sync_pointer(bridge_dir, up_dir, cfg)
    else:
        print(sync_explicit(bridge_dir, up_dir, cfg, bin_env))
    write_upstream_md(bridge_dir, cfg, ref, sha, mode)

    run([os.environ.get("HARNESSATOR_BIN", "harnessator"),
         "validate", "--check-paths", "harnessator.toml"],
        cwd=bridge_dir)
    print("validate --check-paths: ok")

    dirty = run(["git", "status", "--porcelain"], cwd=bridge_dir).stdout.strip()
    if not dirty:
        print("already current - nothing to commit"
              + (f"; refusing to tag an unchanged tree with {args.new_tag!r}"
                 if args.new_tag else ""))
        return 0

    short = run(["git", "diff", "--stat"], cwd=bridge_dir).stdout.strip().splitlines()
    print(f"changes: {len(dirty.splitlines())} file(s)\n" +
          (short[-1] if short else ""))
    if args.dry:
        print("dry run - committing nothing, pushing nothing")
        return 0

    ident = ["-c", "user.name=Roland Aigner",
             "-c", "user.email=roland@aignerdevelopment.com"]
    run(["git", "add", "-A"], cwd=bridge_dir)
    run(["git"] + ident + ["commit", "-qm",
          f"upstream sync: {ref} ({sha[:12]})"], cwd=bridge_dir)
    run(["git"] + ident + ["push", "origin", "main"], cwd=bridge_dir, env=env)
    if args.new_tag:
        run(["git", "tag", "-a", args.new_tag, "-m",
             f"bridge of {cfg['upstream']} @ {sha[:12]}"], cwd=bridge_dir)
        run(["git", "push", "origin", args.new_tag], cwd=bridge_dir, env=env)
    print(f"pushed main{' + tag ' + args.new_tag if args.new_tag else ''}"
          " - consumers move with harnessator update, pinned to tags")
    return 0


if __name__ == "__main__":
    sys.exit(main())
