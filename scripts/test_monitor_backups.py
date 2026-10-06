import importlib.util
import datetime
import hashlib
import json
import os
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location("monitor", pathlib.Path(__file__).with_name("monitor-backups.py"))
monitor = importlib.util.module_from_spec(spec)
spec.loader.exec_module(monitor)


class FreshnessProbeTests(unittest.TestCase):
    def report(self, status="warning", **fields):
        row = dict(status=status, age_seconds=3600, max_age_seconds=7200,
                   artifact_exists=True, stale=False, integrity="unknown")
        row.update(fields)
        return json.dumps(dict(status=status, databases=[row]))

    def test_fresh_without_checksum_history(self):
        self.assertEqual(monitor.evaluate(1, self.report()),
                         ("healthy", "fresh_backup_integrity_unknown", "warning"))

    def test_verified(self):
        self.assertEqual(monitor.evaluate(0, self.report("healthy", integrity="verified"))[0], "healthy")

    def test_overdue_no_backup_storage_and_bad_responses(self):
        for code, text in [(1, self.report("critical", stale=True, age_seconds=8000)),
                           (1, json.dumps(dict(status="critical", databases=[]))),
                           (1, json.dumps(dict(status="unknown", reason="storage unavailable", databases=[]))),
                           (5, json.dumps(dict(status="unknown", databases=[]))),
                           (1, ""), (0, "not JSON"), (0, self.report()),
                           (0, self.report("healthy", integrity="failed")),
                           (1, self.report(artifact_exists=False)),
                           (1, self.report(age_seconds=-1)),
                           (1, self.report(age_seconds=True)),
                           (1, self.report(max_age_seconds=0))]:
            with self.subTest(code=code, text=text):
                self.assertNotEqual(monitor.evaluate(code, text)[0], "healthy")

    def test_timeout_and_spawn_failure(self):
        for error, reason in [(OSError("sensitive diagnostic"), "probe_unavailable"),
                              (subprocess.TimeoutExpired("secret", 30), "probe_timeout")]:
            with mock.patch.object(monitor.subprocess, "run", side_effect=error):
                result = monitor.probe("dbvault", "profile.yaml", 30)
                self.assertEqual(result[0:2], ("unknown", reason))
                self.assertNotIn("secret", str(result))

    def test_probe_disables_local_state_and_discards_diagnostics(self):
        completed = subprocess.CompletedProcess([], 1, self.report().encode(), b"secret")
        with mock.patch.object(monitor.subprocess, "run", return_value=completed) as run:
            result = monitor.probe("dbvault", "profile.yaml", 30)
            self.assertEqual(result[0], "healthy")
            self.assertEqual(run.call_args.args[0][4:6], ["--state", ""])
            self.assertIs(run.call_args.kwargs["stderr"], subprocess.DEVNULL)
            self.assertNotIn("secret", str(result))


@unittest.skipUnless(os.environ.get("DBVAULT_MONITOR_BINARY"), "Set DBVAULT_MONITOR_BINARY for real CLI checks")
class RealCLIProbeTests(unittest.TestCase):
    def test_registered_backups_without_producer_or_database(self):
        binary = os.environ["DBVAULT_MONITOR_BINARY"]
        for case in ("fresh", "overdue", "empty", "storage", "native-fresh", "native-overdue", "native-other-source"):
            with self.subTest(case=case), tempfile.TemporaryDirectory() as temp:
                root = pathlib.Path(temp)
                store = root / "storage"
                if case == "storage":
                    store.write_text("not a directory")
                else:
                    store.mkdir()
                native = case.startswith("native-")
                age = 8 if "overdue" in case else 1
                now = datetime.datetime.now(datetime.timezone.utc)
                at = (now - datetime.timedelta(hours=age)).isoformat()
                config = root / "monitor.yaml"
                profile = ("version: '1'\ndatabase:\n  type: " + ("mysql" if native else "postgres")
                           + "\n  database: production\n  user: unused\n  password_env: UNUSED_MONITOR_PASSWORD\n"
                           + "storage:\n  local:\n    path: " + store.as_posix()
                           + "\nhealth:\n  max_backup_age: 7h\n")
                if native:
                    profile += "  backup_scope: pitr\n  source_identity: native-source\n"
                config.write_text(profile)
                payload = b"opaque encrypted or plain stored bytes"
                if case not in ("empty", "storage"):
                    if native:
                        name = "pitr_00000000000000000000000000000001.tar.enc"
                        record = dict(version=1, name=name, engine="mysql", identity="native-source",
                                      server_version="8.4.0", gtid_mode="OFF", kind="base",
                                      start="binlog.000001:4", end="binlog.000001:4",
                                      **{"from": at, "until": at, "bytes": len(payload),
                                         "sha256": hashlib.sha256(payload).hexdigest(),
                                         "key_id": "unavailable-key", "algorithm": "aes256-gcm-stream-v2"})
                        if case == "native-other-source":
                            record["identity"] = "different-source"
                        suffix = ".pitr.json"
                    else:
                        name, suffix = "fixture.dump", ".meta.json"
                        record = dict(manifest_version="1.0", backup_id="fixture", backup_name=name, backup_type="full",
                                      created_at=at, completed_at=at, status="completed",
                                      database=dict(engine="postgres", database_name="production", format="custom"),
                                      pipeline=dict(compression="none", compression_level=6, compressed_bytes=len(payload), uncompressed_bytes=len(payload)),
                                      checksum=dict(algorithm="sha256", hash=hashlib.sha256(payload).hexdigest()))
                    (store / name).write_bytes(payload)
                    (store / (name + suffix)).write_text(json.dumps(record))
                raw = subprocess.run([binary, "health", "--config", str(config), "--state", "", "--json"],
                                     capture_output=True, timeout=45)
                body = json.loads(raw.stdout)
                want = "warning" if case in ("fresh", "native-fresh") else ("unknown" if case == "storage" else "critical")
                self.assertEqual(body["status"], want, raw.stderr.decode(errors="replace"))
                self.assertEqual(raw.returncode, 1)
                result = subprocess.run([sys.executable, str(pathlib.Path(__file__).with_name("monitor-backups.py")),
                                         "--dbvault", binary, "--config", str(config)], capture_output=True, timeout=45)
                probe = json.loads(result.stdout)
                good = case in ("fresh", "native-fresh")
                self.assertEqual(result.returncode, 0 if good else 1)
                self.assertEqual(probe["status"], "healthy" if good else want)
                self.assertEqual(result.stderr, b"")


if __name__ == "__main__":
    unittest.main()
