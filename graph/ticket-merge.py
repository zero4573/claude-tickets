"""Graph merger of the ticket graph server (graph/serve.sh, ct graph mcp).

Runs in the claude-tickets graphify image with:
  PROJECT_ROOT   the ticket workspace (~/work/<ID>), read-write
  PROJECTS_ROOT  ~/Projects, read-only; main clones at <provider>/<owner>/<repo>

Serves one graph for the whole system so Claude needs one MCP server, not
one per repo: for every repo with a worktree listed in
$PROJECT_ROOT/workspace.json (written by ct ws), that worktree's graph
(kept current with `graphify update` + `graphify watch`); for every other
repo, its main clone's graph (built on the host by ct graph index). They are
combined with `graphify merge-graphs` into
$PROJECT_ROOT/graphify-out/graph.json. merge-graphs prefixes node ids with
a repo tag taken from the folder holding each graphify-out/, without
resolving symlinks. Worktrees already live in folders named by slug
(~/work/<ID>/<slug>), and main clones are passed through symlinks
(graphify-out/.inputs/<slug>/graphify-out), so every tag is the repo's
slug, <provider>-<owner>-<repo>, the name the vault uses too. graphify.serve reloads that file
when its mtime changes.

  --initial  one pass (build worktree graphs, merge) and exit
  (default)  loop: start watchers for new worktrees, re-merge whenever an
             input graph or workspace.json changes (debounced)
"""

import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path

POLL_SECONDS = 5
# Quiet time before re-merging: short when a worktree changed (the session's
# own edits), longer when only main-clone graphs did (ct graph index rewrites
# them all at once, and every running session would re-merge together)
DEBOUNCE_SECONDS = 10
MAIN_ONLY_DEBOUNCE_SECONDS = 120

workspace = Path(os.environ["PROJECT_ROOT"])
projects = Path(os.environ.get("PROJECTS_ROOT", str(Path.home() / "Projects")))
out = workspace / "graphify-out" / "graph.json"
workspace_file = workspace / "workspace.json"

# graphify records source_file relative to its working directory, so builds
# and watches run from inside each repo to keep paths repo-relative.
# Worktrees are ticket branches: deleting code is expected, so let graphify
# write a graph with fewer nodes than the one it replaces
worktree_env = dict(os.environ, GRAPHIFY_FORCE="1")

watchers: dict[Path, subprocess.Popen] = {}


def log(msg: str) -> None:
    print(f"ticket-merge: {msg}", flush=True)


def slug_of(*parts: str) -> str:
    """<provider>-<owner>-<repo>, kebab-case: same as repo.Slug in internal/repo."""
    return re.sub(r"[^a-z0-9]+", "-", "-".join(parts).lower()).strip("-")


def worktrees() -> list[tuple[str, Path]]:
    """(slug, path) of each worktree in workspace.json."""
    try:
        data = json.loads(workspace_file.read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return []
    found = []
    for entry in data.get("repos", []):
        p = Path(entry.get("path", ""))
        if p.is_dir():
            found.append((entry.get("slug") or p.name, p))
    return found


def main_clone_graphs(exclude: set[str]) -> list[Path]:
    """Main clones' graphs, each through a symlink folder named by its slug."""
    links = out.parent / ".inputs"
    graphs = []
    for g in sorted(projects.glob("*/*/*/graphify-out/graph.json")):
        repo_dir = g.parent.parent
        slug = slug_of(*repo_dir.relative_to(projects).parts)
        if slug in exclude:
            continue
        link = links / slug / "graphify-out"
        if not (link.is_symlink() and os.readlink(link) == str(g.parent)):
            link.parent.mkdir(parents=True, exist_ok=True)
            if link.is_symlink() or link.exists():
                link.unlink()
            link.symlink_to(g.parent)
        graphs.append(link / "graph.json")
    return graphs


def inputs() -> list[Path]:
    wts = worktrees()
    wt_graphs = [p / "graphify-out" / "graph.json" for _, p in wts]
    return [g for g in wt_graphs if g.exists()] + main_clone_graphs({s for s, _ in wts})


def build_worktree(path: Path) -> None:
    log(f"building graph for {path}")
    subprocess.run(["graphify", "update", "."], cwd=path, env=worktree_env, check=False)


def ensure_watchers() -> None:
    current = {path for _, path in worktrees()}
    # Stop watching repos that left the workspace (ct kb repo reset, ct ws rm)
    for path in list(watchers):
        if path not in current:
            watchers.pop(path).terminate()
            log(f"stopped watching {path}")
    for path in current:
        # ct kb repo graph asks for a rebuild by touching this file
        flag = path / "graphify-out" / ".rebuild"
        if flag.exists():
            flag.unlink()
            build_worktree(path)
        proc = watchers.get(path)
        if proc is not None and proc.poll() is None:
            continue
        # --initial already built it; a restarted watcher catches up by itself
        if proc is None and not (path / "graphify-out" / "graph.json").exists():
            build_worktree(path)
        log(f"watching {path}")
        watchers[path] = subprocess.Popen(["graphify", "watch", "."], cwd=path, env=worktree_env)


def signature(paths: list[Path]) -> tuple:
    sig = []
    for p in [workspace_file, *paths]:
        try:
            st = p.stat()
            sig.append((str(p), st.st_mtime_ns, st.st_size))
        except OSError:
            sig.append((str(p), None, None))
    return tuple(sig)


def merge(paths: list[Path]) -> None:
    tmp = out.with_name("graph.json.merging")
    if len(paths) >= 2:
        result = subprocess.run(
            ["graphify", "merge-graphs", *map(str, paths), "--out", str(tmp)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0 or not tmp.exists():
            log(f"merge failed, keeping the previous graph:\n{result.stdout}{result.stderr}")
            return
    elif len(paths) == 1:
        shutil.copyfile(paths[0], tmp)
    else:
        tmp.write_text(
            json.dumps({"directed": False, "multigraph": False, "graph": {}, "nodes": [], "links": []}),
            encoding="utf-8",
        )
    os.replace(tmp, out)
    log(f"merged {len(paths)} graph(s) into {out}")


def initial() -> None:
    for _, path in worktrees():
        build_worktree(path)
    merge(inputs())


def worktree_changed(old_sig: tuple | None, sig: tuple) -> bool:
    """Whether workspace.json or a worktree graph differs between signatures."""
    if old_sig is None:
        return True
    own = {str(workspace_file)} | {str(p / "graphify-out" / "graph.json") for _, p in worktrees()}
    old = {entry[0]: entry for entry in old_sig}
    return any(entry[0] in own and old.get(entry[0]) != entry for entry in sig)


def loop() -> None:
    ensure_watchers()
    paths = inputs()
    merged_sig = None  # always merge once the first watcher builds settle
    pending_since, pending_sig = None, None
    while True:
        time.sleep(POLL_SECONDS)
        ensure_watchers()
        paths = inputs()
        sig = signature(paths)
        if sig == merged_sig:
            pending_since = None
            continue
        # Wait for the inputs to settle (a watch rebuild, a burst of edits)
        if pending_since is None or sig != pending_sig:
            pending_since, pending_sig = time.monotonic(), sig
            continue
        wait = DEBOUNCE_SECONDS if worktree_changed(merged_sig, sig) else MAIN_ONLY_DEBOUNCE_SECONDS
        if time.monotonic() - pending_since >= wait:
            merge(paths)
            merged_sig, pending_since = sig, None


if __name__ == "__main__":
    out.parent.mkdir(parents=True, exist_ok=True)
    if "--initial" in sys.argv[1:]:
        initial()
    else:
        loop()
