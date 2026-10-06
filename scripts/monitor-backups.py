#!/usr/bin/env python3
"""Example independent freshness probe. Requires Python 3 and the DBVault CLI.

The monitoring platform owns scheduling, alert delivery and missing-probe alerts.
Only bounded status codes are emitted; DBVault diagnostics are never forwarded.
"""
import argparse
import datetime
import json
import math
import subprocess


def evaluate(returncode, output):
    try:
        report = json.loads(output)
        status = report["status"]
        if status not in ("healthy", "warning", "critical", "unknown"):
            raise ValueError("status")
        if status == "critical" and returncode != 0:
            return "critical", "backup_check_failed", status
        if status == "unknown" and returncode != 0:
            return "unknown", "backup_check_unavailable", status
        rows = report["databases"]
        if not isinstance(rows, list) or len(rows) != 1:
            raise ValueError("database")
        row = rows[0]
        age, maximum = row["age_seconds"], row["max_age_seconds"]
        if any(type(v) not in (int, float) or not math.isfinite(v) for v in (age, maximum)):
            raise ValueError("age")
        fresh = (row["status"] == status and row["artifact_exists"] is True
                 and row["stale"] is False and maximum > 0 and 0 <= age <= maximum)
        if fresh and status == "healthy" and returncode == 0 and row["integrity"] == "verified":
            return "healthy", "fresh_backup", status
        # This probe checks freshness. Default health also warns about unknown
        # checksum history; that remains visible as dbvault_status/exit_code.
        if fresh and status == "warning" and returncode == 1 and row["integrity"] == "unknown":
            return "healthy", "fresh_backup_integrity_unknown", status
    except (ValueError, TypeError, KeyError, OverflowError):
        pass
    return "unknown", "invalid_or_inconsistent_response", None


def probe(binary, config, timeout):
    try:
        completed = subprocess.run(
            [binary, "health", "--config", config, "--state", "",
             "--output", "json", "--timeout", str(timeout) + "s"],
            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
            timeout=timeout + 5, check=False)
        status, reason, original = evaluate(completed.returncode, completed.stdout)
        return status, reason, original, completed.returncode
    except subprocess.TimeoutExpired:
        return "unknown", "probe_timeout", None, None
    except OSError:
        return "unknown", "probe_unavailable", None, None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--config", required=True, help="Independent monitor profile with shared storage and max_backup_age")
    parser.add_argument("--dbvault", default="dbvault", help="DBVault executable path")
    parser.add_argument("--timeout", type=int, default=30, help="Positive DBVault deadline in seconds")
    args = parser.parse_args()
    if args.timeout <= 0:
        parser.error("timeout must be positive")
    status, reason, original, code = probe(args.dbvault, args.config, args.timeout)
    print(json.dumps({"status": status, "reason_code": reason,
                      "dbvault_status": original, "dbvault_exit_code": code,
                      "checked_at": datetime.datetime.now(datetime.timezone.utc).isoformat()}))
    return 0 if status == "healthy" else 1


if __name__ == "__main__":
    raise SystemExit(main())
