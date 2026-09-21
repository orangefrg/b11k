"""Exercise deployment failures with a fake Docker CLI; never touch a real stack."""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class BackendScriptsTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="b11k utilities ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        shutil.copytree(ROOT / "scripts", self.root / "scripts")
        for name in ("docker-compose.yml", "config.docker.yaml"):
            shutil.copy(ROOT / name, self.root / name)
        self.bin = self.root / "fake-bin"
        self.bin.mkdir()
        self.log = self.root / "calls.jsonl"
        self.env = dict(os.environ, PATH=f"{self.bin}:{os.environ['PATH']}",
                        FAKE_LOG=str(self.log), BACKEND_HEALTH_TIMEOUT="1")
        self.write_executable("docker", """
import json, os, sys
args = sys.argv[1:]
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps(args) + '\\n')
failure = os.environ.get('FAKE_FAIL')
if args[0] == 'compose':
    op = args[1:]
    while op and op[0] == '-f': op = op[2:]
    if op[0] == 'ps' and '-q' in op:
        if op[-1] == 'b11k-postgis': print('database-container')
        elif not os.environ.get('FAKE_NO_APP'): print('app-container')
    elif op[0] == 'exec':
        sys.stdout.write('PGDMP synthetic dump')
        if failure == 'backup': sys.exit(1)
    elif op[0] == 'build' and failure == 'build': sys.exit(1)
    elif op[0] == 'run' and failure == 'migration': sys.exit(1)
elif args[0] == 'inspect':
    if args[-1] == 'app-container' and failure == 'health': print('unhealthy')
    elif '.Image' in args[2]: print('sha256:actual-container-image')
    else: print('healthy')
elif args[0] == 'run': print('disposable-test-container')
elif args[0] == 'port': print('127.0.0.1:29999')
""")
        self.write_executable("sleep", "")
        self.write_executable("go", """
import json, os, sys
with open(os.environ['FAKE_LOG'], 'a') as log:
    log.write(json.dumps(['go', *sys.argv[1:], os.environ.get('B11K_TEST_DATABASE_URL', '')]) + '\\n')
sys.exit(int(os.environ.get('FAKE_GO_EXIT', '0')))
""")

    def write_executable(self, name, text):
        path = self.bin / name
        path.write_text(f"#!{sys.executable}\n" + text)
        path.chmod(0o755)

    def run_script(self, name, *args, **env):
        return subprocess.run([str(self.root / "scripts" / name), *args],
                              cwd="/tmp", env=dict(self.env, **env),
                              text=True, capture_output=True, timeout=15)

    def calls(self):
        return [json.loads(line) for line in self.log.read_text().splitlines()]

    def compose_calls(self):
        result = []
        for args in self.calls():
            if args[0] != "compose":
                continue
            op = args[1:]
            while op and op[0] == "-f":
                op = op[2:]
            result.append(op)
        return result

    def snapshots(self):
        return sorted(path for path in (self.root / "backups/backend").iterdir() if path.is_dir())

    def test_rebuild_saves_actual_image_and_backup_before_stopping(self):
        result = self.run_script("rebuild-backend")
        self.assertEqual(result.returncode, 0, result.stderr)
        calls = self.compose_calls()
        order = [next(i for i, cmd in enumerate(calls) if cmd[0] == op)
                 for op in ("build", "exec", "stop", "run")]
        self.assertEqual(order, sorted(order))
        self.assertTrue(any(cmd[:3] == ["image", "tag", "sha256:actual-container-image"] for cmd in self.calls()))
        snapshot, = self.snapshots()
        self.assertTrue((snapshot / "postgres.dump").is_file())
        self.assertTrue((snapshot / "deployed.txt").is_file())
        self.assertEqual((snapshot / "postgres.dump").stat().st_mode & 0o077, 0)
        self.assertFalse((self.root / "backups/backend/.operation-lock").exists())
        self.assertNotIn("-force-rebuild", str(calls))
        self.assertFalse(any(cmd[0] == "down" for cmd in calls))

    def test_build_and_backup_failures_leave_app_running(self):
        for failure in ("build", "backup"):
            with self.subTest(failure=failure):
                self.log.unlink(missing_ok=True)
                result = self.run_script("rebuild-backend", FAKE_FAIL=failure)
                self.assertNotEqual(result.returncode, 0)
                self.assertFalse(any(cmd[0] in ("stop", "run") for cmd in self.compose_calls()))
                self.assertFalse((self.root / "backups/backend/.operation-lock").exists())
        self.assertFalse(any((path / "backup-complete.txt").exists() for path in self.snapshots()))

    def test_failed_migration_does_not_start_app(self):
        result = self.run_script("rebuild-backend", FAKE_FAIL="migration")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("remains stopped", result.stderr)
        self.assertFalse(any(cmd[0] == "up" and cmd[-1] == "b11k-app" for cmd in self.compose_calls()))
        snapshot, = self.snapshots()
        self.assertTrue((snapshot / "backup-complete.txt").exists())

    def test_unhealthy_app_is_not_reported_as_success(self):
        result = self.run_script("rebuild-backend", FAKE_FAIL="health")
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("rollback-backend", result.stderr)
        snapshot, = self.snapshots()
        self.assertFalse((snapshot / "deployed.txt").exists())

    def test_rollback_selects_completed_snapshot_and_preserves_database(self):
        result = self.run_script("backup-backend")
        self.assertEqual(result.returncode, 0, result.stderr)
        incomplete = self.root / "backups/backend/20990101T000000Z-999"
        incomplete.mkdir()
        self.log.unlink()
        result = self.run_script("rollback-backend")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertFalse(any(cmd[0] in ("exec", "run", "build", "down") for cmd in self.compose_calls()))
        self.assertIn("Database contents were preserved", result.stdout)

    def test_first_deployment_has_no_rollback_and_rejects_path_traversal(self):
        result = self.run_script("backup-backend", FAKE_NO_APP="1")
        self.assertEqual(result.returncode, 0, result.stderr)
        snapshot, = self.snapshots()
        self.assertNotEqual(self.run_script("rollback-backend", snapshot.name).returncode, 0)
        self.assertNotEqual(self.run_script("rollback-backend", "../../outside").returncode, 0)

    def test_concurrent_operation_lock_is_preserved(self):
        lock = self.root / "backups/backend/.operation-lock"
        lock.mkdir(parents=True)
        result = self.run_script("rebuild-backend")
        self.assertNotEqual(result.returncode, 0)
        self.assertTrue(lock.exists())
        self.assertFalse(any(cmd[0] == "up" for cmd in self.compose_calls()))

    def test_failed_race_tests_remove_only_the_disposable_database(self):
        result = self.run_script("test-backend-race", FAKE_GO_EXIT="7", B11K_TEST_DATABASE_URL="postgres://production.invalid/db")
        self.assertEqual(result.returncode, 7, result.stderr)
        calls = self.calls()
        self.assertIn(["rm", "-f", "-v", "disposable-test-container"], calls)
        go, = [cmd for cmd in calls if cmd[0] == "go"]
        self.assertIn("-race", go)
        self.assertIn("127.0.0.1:29999", go[-1])
        self.assertNotIn("production.invalid", go[-1])
        self.assertFalse(any(cmd[0] == "compose" for cmd in calls))

    @unittest.skipUnless(shutil.which("rsync"), "rsync is required")
    def test_rsync_copies_runtime_and_preserves_server_secrets_with_delete(self):
        source = self.root / "source"
        target = self.root / "destination"
        source.mkdir()
        target.mkdir()
        included = ["cmd/main.go", "internal/sync/jobs.go", "web/static/app.js",
                    "scripts/rebuild-backend", "scripts/lib/backend.sh", "go.mod",
                    "Dockerfile", ".dockerignore", ".env.example", "config.docker.yaml"]
        excluded = [".env", "config.yaml", "iosApp/App.swift", "dist/apple/app.ipa",
                    "secrets/key.p8", "backups/backend/private.dump", ".git/config",
                    "internal/sync/jobs_test.go", "internal/testdb/testdb.go",
                    "internal/sync/.env", "web/static/secrets/private.txt",
                    "scripts/release-apple", "scripts/tests/test_backend_scripts.py",
                    "web/tests/app.test.cjs", "package.json", "pnpm-lock.yaml",
                    "coverage/frontend/summary.json"]
        for name in included + excluded:
            path = source / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("source")
        protected = [".env", "config.yaml", "backups/backend/keep.dump"]
        for name in protected + ["cmd/obsolete.go"]:
            path = target / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text("server")
        subprocess.run(["rsync", "-a", "--delete", f"--filter=merge {ROOT / 'deploy/backend.rsync-filter'}",
                        str(source) + "/", str(target) + "/"], check=True, capture_output=True)
        for name in included:
            self.assertEqual((target / name).read_text(), "source", name)
        for name in protected:
            self.assertEqual((target / name).read_text(), "server", name)
        for name in set(excluded) - set(protected):
            self.assertFalse((target / name).exists(), name)
        self.assertFalse((target / "cmd/obsolete.go").exists())


if __name__ == "__main__":
    unittest.main()
