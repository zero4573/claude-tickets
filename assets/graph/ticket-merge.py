"""Graph merger of the ticket graph server (graph/serve.sh, ct graph mcp).

Runs in the claude-tickets graphify image with:
  PROJECT_ROOT   the ticket workspace (~/work/<ID>), read-write
  PROJECTS_ROOT  ~/Projects, read-only; main clones at <provider>/<owner>/<repo>

Serves one graph for the whole system so Claude needs one MCP server, not
one per repo: for every repo with a worktree listed in
$PROJECT_ROOT/workspace.json (written by ct ws), that worktree's graph
(kept current with `graphify update` + `graphify watch`, writing to
$PROJECT_ROOT/.cache/graphify/<slug>/graphify-out through GRAPHIFY_OUT, so
nothing lands in the worktree); for every other
repo, its main clone's graph (built on the host by ct graph index). They are
combined with `graphify merge-graphs` into
$PROJECT_ROOT/graphify-out/graph.json. merge-graphs prefixes node ids with
a repo tag taken from the folder holding each graphify-out/, without
resolving symlinks. Worktree graphs live in folders named by slug
(.cache/graphify/<slug>/graphify-out), and main clones are passed through
symlinks (graphify-out/.inputs/<slug>/graphify-out), so every tag is the repo's
slug, <provider>-<owner>-<repo>, the name the vault uses too. graphify.serve reloads that file
when its mtime changes.

  --initial  one pass (build worktree graphs, merge unless the merged graph
             was made of the same inputs, by sha256) and exit, non-zero
             when the merge failed
  (default)  loop: start watchers for new worktrees, re-merge whenever an
             input graph or workspace.json changes (debounced)
"""

import hashlib
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

# (no failure on import: the tests import this module without the env)
workspace = Path(os.environ.get("PROJECT_ROOT", "."))
projects = Path(os.environ.get("PROJECTS_ROOT", str(Path.home() / "Projects")))
out = workspace / "graphify-out" / "graph.json"
# What the merged graph was made of (stamp_of), so --initial can tell it's current
stamp_file = workspace / "graphify-out" / ".merged-inputs.json"
workspace_file = workspace / "workspace.json"

# graphify records source_file relative to its working directory, so builds
# and watches run from inside each repo to keep paths repo-relative.
# Worktrees are ticket branches: deleting code is expected, so let graphify
# write a graph with fewer nodes than the one it replaces. Each worktree's
# output goes outside it (worktree_env), where no formatter walks.
base_env = dict(os.environ, GRAPHIFY_FORCE="1")

watchers: dict[Path, subprocess.Popen] = {}
# worktree path -> slug, refreshed from workspace.json by worktrees()
slugs: dict[Path, str] = {}
# spec exploration clones (mode "explore") keep <clone>/graphify-out
explore: set[Path] = set()
# Unknown paths already logged by out_dir (the poll loop asks often)
unknown: set[Path] = set()


def worktree_out(slug: str) -> Path:
    """Where a ticket worktree's graphify output lives (internal/graph.WorktreeOut)."""
    return workspace / ".cache" / "graphify" / slug / "graphify-out"


def out_dir(path: Path) -> Path | None:
    """The graphify-out dir of the checkout at path, None for an unknown path."""
    if path in explore:
        return path / "graphify-out"
    slug = slugs.get(path)
    if slug is None:
        if path not in unknown:
            unknown.add(path)
            log(f"skipping {path}: not in workspace.json")
        return None
    return worktree_out(slug)


def worktree_env(path: Path) -> dict[str, str]:
    out_path = out_dir(path)
    if path in explore or out_path is None:
        return dict(base_env)
    return dict(base_env, GRAPHIFY_OUT=str(out_path))


def migrate_legacy(slug: str, path: Path) -> None:
    """Move a pre-existing <worktree>/graphify-out to the new place, or drop it."""
    old = path / "graphify-out"
    if not (old.is_dir() and not old.is_symlink()):
        return
    new = worktree_out(slug)
    try:
        if not new.exists() or not any(new.iterdir()):
            new.parent.mkdir(parents=True, exist_ok=True)
            if new.exists():
                new.rmdir()
            shutil.move(str(old), str(new))
            log(f"moved {old} to {new}")
        else:
            shutil.rmtree(old)
            log(f"removed legacy {old}")
    except OSError as e:
        log(f"couldn't migrate {old}: {e}")


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
            slug = entry.get("slug") or p.name
            if entry.get("mode") == "explore":
                explore.add(p)
            else:
                explore.discard(p)
            if slugs.get(p) != slug:
                slugs[p] = slug
                if p not in explore:
                    migrate_legacy(slug, p)
            found.append((slug, p))
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
    wt_graphs = [out_dir(p) / "graph.json" for _, p in wts if out_dir(p)]
    return [g for g in wt_graphs if g.exists()] + main_clone_graphs({s for s, _ in wts})


def main_clone_of(slug: str) -> Path | None:
    """The main clone for a repo slug, found under <provider>/<owner>/<repo>."""
    for d in sorted(projects.glob("*/*/*")):
        if d.is_dir() and slug_of(*d.relative_to(projects).parts) == slug:
            return d
    return None


def seed_from_main(slug: str, dest: Path) -> None:
    """Copy the main clone's graph, manifest and cache into dest (graph.Seed in Go)."""
    main = main_clone_of(slug)
    src = main / "graphify-out" if main else None
    if src is None or not src.is_dir() or (dest / "graph.json").exists():
        return
    try:
        dest.mkdir(parents=True, exist_ok=True)
        # graph.json last: it marks a complete seed, so a failed copy is retried
        for name in ("cache", "manifest.json", "graph.json"):
            s, d = src / name, dest / name
            if s.is_dir():
                shutil.copytree(s, d, dirs_exist_ok=True)
            elif s.is_file():
                shutil.copyfile(s, d)
        log(f"seeded {dest} from {src}")
    except OSError as e:
        (dest / "graph.json").unlink(missing_ok=True)
        log(f"couldn't seed {dest} from {src}: {e}")


def build_worktree(path: Path) -> None:
    dest = out_dir(path)
    if dest is None:
        return
    log(f"building graph for {path}")
    dest.mkdir(parents=True, exist_ok=True)
    if path not in explore and not (dest / "graph.json").exists():
        # A gc'd workspace lost its .cache: start from the main clone's graph
        # and let the incremental update below catch up
        seed_from_main(slugs[path], dest)
    subprocess.run(["graphify", "update", "."], cwd=path, env=worktree_env(path), check=False)


def ensure_watchers() -> None:
    current = {path for _, path in worktrees()}
    # Stop watching repos that left the workspace (ct spec repo reset, ct ws rm)
    for path in list(watchers):
        if path not in current:
            watchers.pop(path).terminate()
            log(f"stopped watching {path}")
    for path in current:
        # ct spec repo graph asks for a rebuild by touching this file
        dest = out_dir(path)
        if dest is None:
            continue
        flag = dest / ".rebuild"
        if flag.exists():
            flag.unlink()
            build_worktree(path)
        proc = watchers.get(path)
        if proc is not None and proc.poll() is None:
            continue
        # --initial already built it; a restarted watcher catches up by itself
        if proc is None and not (dest / "graph.json").exists():
            build_worktree(path)
        log(f"watching {path}")
        watchers[path] = subprocess.Popen(["graphify", "watch", "."], cwd=path, env=worktree_env(path))


def signature(paths: list[Path]) -> tuple:
    sig = []
    for p in [workspace_file, *paths]:
        try:
            st = p.stat()
            sig.append((str(p), st.st_mtime_ns, st.st_size))
        except OSError:
            sig.append((str(p), None, None))
    return tuple(sig)


def stamp_of(paths: list[Path]) -> list[list[str]]:
    """sha256 of workspace.json and each input graph: what a merge was made of."""
    stamp = []
    for p in [workspace_file, *paths]:
        h = hashlib.sha256()
        try:
            with open(p, "rb") as f:
                for chunk in iter(lambda: f.read(1 << 20), b""):
                    h.update(chunk)
        except OSError:
            continue
        stamp.append([str(p), h.hexdigest()])
    return stamp


def is_current(paths: list[Path]) -> bool:
    """Whether the merged graph was made of exactly these inputs, unchanged."""
    try:
        return out.exists() and json.loads(stamp_file.read_text(encoding="utf-8")) == stamp_of(paths)
    except (OSError, ValueError):
        return False


def merge(paths: list[Path]) -> bool:
    tmp = out.with_name("graph.json.merging")
    # Hashed before merging: an input that changes during the merge then
    # makes the next check see a stale graph, not a current one
    stamp = stamp_of(paths)
    if len(paths) >= 2:
        result = subprocess.run(
            ["graphify", "merge-graphs", *map(str, paths), "--out", str(tmp)],
            capture_output=True,
            text=True,
        )
        if result.returncode != 0 or not tmp.exists():
            log(f"merge failed, keeping the previous graph:\n{result.stdout}{result.stderr}")
            return False
    elif len(paths) == 1:
        shutil.copyfile(paths[0], tmp)
    else:
        tmp.write_text(
            json.dumps({"directed": False, "multigraph": False, "graph": {}, "nodes": [], "links": []}),
            encoding="utf-8",
        )
    os.replace(tmp, out)
    stamp_file.write_text(json.dumps(stamp), encoding="utf-8")
    log(f"merged {len(paths)} graph(s) into {out}")
    return True


def initial() -> bool:
    for _, path in worktrees():
        build_worktree(path)
    paths = inputs()
    if is_current(paths):
        log(f"{out} is current ({len(paths)} graph(s) unchanged since its merge)")
        return True
    return merge(paths)


def worktree_changed(old_sig: tuple | None, sig: tuple) -> bool:
    """Whether workspace.json or a worktree graph differs between signatures."""
    if old_sig is None:
        return True
    own = {str(workspace_file)} | {str(out_dir(p) / "graph.json") for _, p in worktrees() if out_dir(p)}
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
            # (a touched but unchanged input, or the first pass after
            # --initial, needs no new merge)
            if not is_current(paths):
                merge(paths)
            merged_sig, pending_since = sig, None


if __name__ == "__main__":
    out.parent.mkdir(parents=True, exist_ok=True)
    if "--initial" in sys.argv[1:]:
        sys.exit(0 if initial() else 1)
    else:
        loop()
