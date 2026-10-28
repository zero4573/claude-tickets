import importlib.util
import os
import tempfile
import unittest
from pathlib import Path

SPEC = importlib.util.spec_from_file_location("ticket_merge", Path(__file__).with_name("ticket-merge.py"))


class MergeTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        root = Path(self.tmp.name)
        self.ws, self.proj = root / "ws", root / "proj"
        self.ws.mkdir()
        self.proj.mkdir()
        os.environ["PROJECT_ROOT"] = str(self.ws)
        os.environ["PROJECTS_ROOT"] = str(self.proj)
        self.m = importlib.util.module_from_spec(SPEC)
        SPEC.loader.exec_module(self.m)

    def wt(self, name="a-b-c"):
        p = self.ws / name
        p.mkdir()
        self.m.slugs[p] = name
        return p

    def legacy(self, p):
        (p / "graphify-out").mkdir()
        (p / "graphify-out" / "graph.json").write_text("old")

    def test_legacy_moved_into_missing_dir(self):
        p = self.wt()
        self.legacy(p)
        self.m.migrate_legacy("a-b-c", p)
        self.assertFalse((p / "graphify-out").exists())
        self.assertEqual((self.m.worktree_out("a-b-c") / "graph.json").read_text(), "old")

    def test_legacy_moved_into_empty_dir(self):
        p = self.wt()
        self.legacy(p)
        self.m.worktree_out("a-b-c").mkdir(parents=True)
        self.m.migrate_legacy("a-b-c", p)
        self.assertTrue((self.m.worktree_out("a-b-c") / "graph.json").exists())

    def test_legacy_dropped_when_new_non_empty(self):
        p = self.wt()
        self.legacy(p)
        new = self.m.worktree_out("a-b-c")
        new.mkdir(parents=True)
        (new / "graph.json").write_text("new")
        self.m.migrate_legacy("a-b-c", p)
        self.assertFalse((p / "graphify-out").exists())
        self.assertEqual((new / "graph.json").read_text(), "new")

    def test_legacy_symlink_skipped(self):
        p = self.wt()
        target = Path(self.tmp.name) / "elsewhere"
        target.mkdir()
        (p / "graphify-out").symlink_to(target)
        self.m.migrate_legacy("a-b-c", p)
        self.assertTrue((p / "graphify-out").is_symlink())
        self.assertFalse(self.m.worktree_out("a-b-c").exists())

    def test_explore_clone_untouched(self):
        p = self.wt()
        self.m.explore.add(p)
        self.assertEqual(self.m.out_dir(p), p / "graphify-out")
        self.assertNotIn("GRAPHIFY_OUT", self.m.worktree_env(p))

    def test_unknown_path_skipped(self):
        p = self.ws / "nope"
        self.assertIsNone(self.m.out_dir(p))
        self.assertNotIn("GRAPHIFY_OUT", self.m.worktree_env(p))
        self.m.build_worktree(p)  # must not raise

    def test_reseed_from_main_clone(self):
        main_out = self.proj / "bb" / "own" / "repo" / "graphify-out"
        (main_out / "cache").mkdir(parents=True)
        (main_out / "graph.json").write_text("g")
        (main_out / "manifest.json").write_text("m")
        (main_out / "cache" / "x").write_text("c")
        (main_out / "other").write_text("skip")
        dest = self.m.worktree_out("bb-own-repo")
        self.m.seed_from_main("bb-own-repo", dest)
        self.assertEqual((dest / "graph.json").read_text(), "g")
        self.assertEqual((dest / "manifest.json").read_text(), "m")
        self.assertEqual((dest / "cache" / "x").read_text(), "c")
        self.assertFalse((dest / "other").exists())

    def test_no_reseed_over_existing_graph(self):
        main_out = self.proj / "bb" / "own" / "repo" / "graphify-out"
        main_out.mkdir(parents=True)
        (main_out / "graph.json").write_text("g")
        dest = self.m.worktree_out("bb-own-repo")
        dest.mkdir(parents=True)
        (dest / "graph.json").write_text("mine")
        self.m.seed_from_main("bb-own-repo", dest)
        self.assertEqual((dest / "graph.json").read_text(), "mine")

    def test_failed_seed_leaves_no_graph(self):
        main_out = self.proj / "bb" / "own" / "repo" / "graphify-out"
        main_out.mkdir(parents=True)
        (main_out / "graph.json").write_text("g")
        (main_out / "manifest.json").write_text("m")
        dest = self.m.worktree_out("bb-own-repo")
        real = self.m.shutil.copyfile

        def fail_manifest(s, d, *a, **k):
            if Path(s).name == "manifest.json":
                raise OSError("disk full")
            return real(s, d, *a, **k)

        self.m.shutil.copyfile = fail_manifest
        try:
            self.m.seed_from_main("bb-own-repo", dest)
        finally:
            self.m.shutil.copyfile = real
        self.assertFalse((dest / "graph.json").exists())
        self.m.seed_from_main("bb-own-repo", dest)
        self.assertEqual((dest / "graph.json").read_text(), "g")


if __name__ == "__main__":
    unittest.main()
