"""vault-links: check and preserve Obsidian wikilinks when moving notes.

Usage:
  vault-links check <vault> [file...]
      Report wikilinks/embeds ([[x]], ![[x]], [[x#heading]], [[x|alias]])
      that resolve to no note, or to more than one note (ambiguous by bare
      name). Without files, checks the whole vault except templates/.
      Exits 1 if anything is broken.
  vault-links move <vault> <src> <dst>
      Move a note (or attachment) inside the vault, keeping every link to
      it working: refuses if the destination's name is already taken by
      another file (bare-name links would become ambiguous), then rewrites
      any path-qualified links ([[old/path/note]]) across the vault to the
      new path. Bare-name links ([[note]]) need no rewrite. <dst> may be a
      directory. Prints the new path.

Resolution follows Obsidian: a link target without "/" matches by file
name (case-insensitive, ".md" implied for notes); with "/" it matches the
end of the vault-relative path. Links inside code blocks and inline code
are ignored. .obsidian/, .trash/ and other dot-folders are skipped.
"""

import re
import shutil
import sys
from pathlib import Path

LINK_RE = re.compile(r"(!?)\[\[([^\[\]|#^]*)([#^][^\[\]|]*)?(\|[^\[\]]*)?\]\]")
FENCE_RE = re.compile(r"^\s*(```|~~~)")
INLINE_CODE_RE = re.compile(r"`[^`\n]*`")


def die(msg: str) -> None:
    print(f"vault-links: {msg}", file=sys.stderr)
    sys.exit(2)


def vault_files(vault: Path) -> list[Path]:
    files = []
    for p in vault.rglob("*"):
        rel = p.relative_to(vault)
        if any(part.startswith(".") for part in rel.parts):
            continue
        if p.is_file():
            files.append(p)
    return files


class Index:
    def __init__(self, vault: Path):
        self.vault = vault
        self.by_name: dict[str, list[Path]] = {}
        self.rel_paths: list[tuple[str, Path]] = []
        for p in vault_files(vault):
            rel = p.relative_to(vault).as_posix()
            for key in self._keys(p):
                self.by_name.setdefault(key, []).append(p)
            self.rel_paths.append((rel.lower(), p))

    @staticmethod
    def _keys(p: Path) -> list[str]:
        keys = [p.name.lower()]
        if p.suffix == ".md":
            keys.append(p.stem.lower())
        return keys

    def resolve(self, target: str) -> list[Path]:
        target = target.strip()
        if not target:
            return []  # [[#heading]]: a link within the same note
        t = target.lower()
        if "/" not in t:
            return self.by_name.get(t, [])
        t = t.lstrip("/")
        candidates = {t, t + ".md"}
        return [p for rel, p in self.rel_paths if any(rel == c or rel.endswith("/" + c) for c in candidates)]


def links_in(text: str):
    """Yield (line_no, match) for wikilinks outside code."""
    in_fence = False
    for no, line in enumerate(text.splitlines(), 1):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            continue
        if in_fence:
            continue
        scrubbed = INLINE_CODE_RE.sub(lambda m: " " * len(m.group(0)), line)
        for m in LINK_RE.finditer(scrubbed):
            yield no, m


def sub_outside_code(text: str, repl) -> str:
    """LINK_RE.sub(repl, text), leaving code blocks and inline code alone."""
    out = []
    in_fence = False
    for line in text.splitlines(keepends=True):
        if FENCE_RE.match(line):
            in_fence = not in_fence
            out.append(line)
            continue
        if in_fence:
            out.append(line)
            continue
        # re.split with a capture group: odd items are the inline code spans
        parts = re.split(f"({INLINE_CODE_RE.pattern})", line)
        out.append("".join(p if i % 2 else LINK_RE.sub(repl, p) for i, p in enumerate(parts)))
    return "".join(out)


def check(vault: Path, files: list[Path]) -> int:
    index = Index(vault)
    if not files:
        files = [p for p in vault_files(vault) if p.suffix == ".md" and p.relative_to(vault).parts[0] != "templates"]
    problems = 0
    for f in files:
        if not f.exists():
            print(f"{f}: file not found")
            problems += 1
            continue
        for no, m in links_in(f.read_text(encoding="utf-8")):
            target = m.group(2)
            if not target.strip():
                continue
            found = index.resolve(target)
            rel = f.relative_to(vault) if f.is_relative_to(vault) else f
            if not found:
                print(f"{rel}:{no}: unresolved link [[{target}]]")
                problems += 1
            elif len(found) > 1:
                where = ", ".join(str(p.relative_to(vault)) for p in found)
                print(f"{rel}:{no}: ambiguous link [[{target}]] -> {where}")
                problems += 1
    if problems == 0:
        print(f"vault-links: {len(files)} file(s) checked, all links resolve")
    return 1 if problems else 0


def move(vault: Path, src: Path, dst: Path) -> int:
    src = src if src.is_absolute() else vault / src
    dst = dst if dst.is_absolute() else vault / dst
    src, vault = src.resolve(), vault.resolve()
    if not src.is_file():
        die(f"no such file: {src}")
    if dst.is_dir() or str(dst).endswith("/"):
        dst = dst / src.name
    dst = dst.resolve()
    if not src.is_relative_to(vault) or not dst.is_relative_to(vault):
        die("source and destination must both be inside the vault")
    if dst.exists():
        die(f"destination exists: {dst.relative_to(vault)}")
    index = Index(vault)
    clashes = [p for key in Index._keys(dst) for p in index.by_name.get(key, []) if p.resolve() != src]
    if clashes:
        where = ", ".join(sorted({str(p.relative_to(vault)) for p in clashes}))
        die(f"name '{dst.name}' is already used by {where}; links to it would become ambiguous")

    old_rel = src.relative_to(vault).as_posix()
    new_rel = dst.relative_to(vault).as_posix()
    old_forms = {old_rel.lower()}
    if src.suffix == ".md":
        old_forms.add(old_rel[:-3].lower())
    new_target = new_rel[:-3] if dst.suffix == ".md" else new_rel

    dst.parent.mkdir(parents=True, exist_ok=True)
    shutil.move(str(src), str(dst))

    rewritten = 0
    for f in vault_files(vault):
        if f.suffix != ".md":
            continue
        text = f.read_text(encoding="utf-8")

        def repl(m: re.Match) -> str:
            nonlocal rewritten
            # A path-qualified target matches the end of the path, so
            # [[sequences/flow]] points at projects/x/sequences/flow.md too
            target = m.group(2).strip().lstrip("/").lower()
            if "/" in target and any(form == target or form.endswith("/" + target) for form in old_forms):
                rewritten += 1
                return f"{m.group(1)}[[{new_target}{m.group(3) or ''}{m.group(4) or ''}]]"
            return m.group(0)

        new_text = sub_outside_code(text, repl)
        if new_text != text:
            f.write_text(new_text, encoding="utf-8")
    print(new_rel)
    if rewritten:
        print(f"vault-links: rewrote {rewritten} path-qualified link(s)", file=sys.stderr)
    return 0


def main(argv: list[str]) -> int:
    if len(argv) < 2 or argv[0] in ("-h", "--help"):
        print(__doc__)
        return 0 if argv and argv[0] in ("-h", "--help") else 1
    cmd, vault = argv[0], Path(argv[1]).resolve()
    if not (vault / ".obsidian").is_dir():
        die(f"not an Obsidian vault: {vault}")
    if cmd == "check":
        files = [Path(a) if Path(a).is_absolute() or Path(a).exists() else vault / a for a in argv[2:]]
        return check(vault, [f.resolve() if f.exists() else f for f in files])
    if cmd == "move" and len(argv) == 4:
        return move(vault, Path(argv[2]), Path(argv[3]))
    print(__doc__)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
