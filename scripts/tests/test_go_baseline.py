import importlib.util
from pathlib import Path
import subprocess
import tempfile
import unittest

SCRIPT = Path(__file__).resolve().parents[1] / "check-go-baseline.py"
spec = importlib.util.spec_from_file_location("baseline", SCRIPT)
baseline = importlib.util.module_from_spec(spec)
spec.loader.exec_module(baseline)


class BaselineTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.run_git("init")
        self.run_git("config", "user.name", "Fixture")
        self.run_git("config", "user.email", "fixture@example.invalid")
        self.write("server/res/sql/migration/mysql/001.sql", "SELECT 1;\n")
        self.write("server/routers/login/login.py", '''raise RuntimeError("must never import")
@router.post("")
@router.post("/")
async def login():
    pass
''')
        self.write("agent/main.py", "# retained Python agent\n")
        self.run_git("add", ".")
        self.run_git("commit", "-m", "fixture")
        self.ref = self.run_git("rev-parse", "HEAD").strip()

    def run_git(self, *args):
        return subprocess.check_output(["git", "-C", str(self.root), *args], stderr=subprocess.DEVNULL).decode()

    def write(self, path, text):
        target = self.root / path
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text, encoding="utf-8")

    def test_inventory_does_not_import_server(self):
        result = baseline.verify(self.root, self.ref)
        self.assertEqual(result["checked_files"], 3)
        self.assertEqual([r["path"] for r in result["routes"]], ["{CONTEXT}/login", "{CONTEXT}/login/"])
        self.assertTrue(all(len(f["sha256"]) == 64 for f in result["frozen_files"]))

    def test_changed_sql_is_rejected(self):
        self.write("server/res/sql/migration/mysql/001.sql", "DROP TABLE users;\n")
        with self.assertRaisesRegex(ValueError, "changed: server/res/sql"):
            baseline.verify(self.root, self.ref)

    def test_deleted_source_is_rejected(self):
        self.run_git("rm", "agent/main.py")
        with self.assertRaisesRegex(ValueError, "deleted: agent/main.py"):
            baseline.verify(self.root, self.ref)

    def test_new_legacy_migration_is_rejected(self):
        self.write("server/res/sql/migration/mysql/002.sql", "SELECT 2;\n")
        self.run_git("add", ".")
        with self.assertRaisesRegex(ValueError, "added: server/res/sql"):
            baseline.verify(self.root, self.ref)

    def test_new_go_files_are_allowed(self):
        self.write("internal/store/store.go", "package store\n")
        self.run_git("add", ".")
        self.assertEqual(baseline.verify(self.root, self.ref)["checked_files"], 3)

    def test_missing_history_fails_closed(self):
        with self.assertRaisesRegex(ValueError, "baseline unavailable"):
            baseline.verify(self.root, "0" * 40)

    def test_symlink_does_not_read_external_content(self):
        target = self.root / "agent/main.py"
        target.unlink()
        try:
            target.symlink_to(self.root / "does-not-exist")
        except OSError:
            self.skipTest("symlinks unavailable")
        with self.assertRaisesRegex(ValueError, "symlink: agent/main.py"):
            baseline.verify(self.root, self.ref)


if __name__ == "__main__":
    unittest.main()
