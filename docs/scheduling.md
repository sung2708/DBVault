# Scheduling & Automated Backups

This guide outlines strategies and configurations for automating periodic database backups using DBVault.

---

## 1. Scheduling Philosophy: Native Schedulers vs In-Process Daemon

DBVault is designed primarily as a single-execution CLI binary that integrates cleanly with industry-standard operating system and orchestrator schedulers.

| Scheduler | Recommended Use Case | Reliability & Observability | Status |
|---|---|---|:---:|
| **Linux Cron (`crontab`)** | Standalone Linux VMs / Bare-Metal | Standard UNIX logging via syslog | Supported |
| **systemd Timers** | Modern Linux distributions | Native logging via `journalctl`, retry control | Supported |
| **Kubernetes `CronJob`** | Containerized microservice clusters | Declarative Pod lifecycle, Prometheus metrics | Supported |
| **Windows Task Scheduler** | Windows Server environments | Native Windows Event Viewer logging | Supported |
| **In-Process Daemon (`dbvault schedule`)** | Lightweight environments without OS cron | robfig/cron, foreground supervised process | Implemented |

---

## 2. Linux `cron` Setup

Edit the backup operator user's crontab:
```bash
crontab -e
```

Add an entry to trigger backups nightly at 02:00 AM:
```cron
# Run daily at 02:00 AM, logging output to /var/log/dbvault.log
0 2 * * * /usr/local/bin/dbvault backup --config /etc/dbvault/postgres.yaml >> /var/log/dbvault.log 2>&1
```

Supply `DB_PASSWORD` through a protected scheduler environment; do not place
credential values in the crontab. The systemd example below uses a restricted
environment file. Container image names in the examples are deployment
placeholders; build and push your reviewed image before using them.

---

## 3. `systemd` Timer Setup (Production Recommended for Linux)

`systemd` timers provide better isolation, execution limits, and logging than traditional cron.

### Service Unit (`/etc/systemd/system/dbvault.service`)
```ini
[Unit]
Description=DBVault Database Backup Execution
After=network-online.target

[Service]
Type=oneshot
User=backup-operator
Group=backup-operator
EnvironmentFile=/etc/dbvault/dbvault.env
ExecStart=/usr/local/bin/dbvault backup --config /etc/dbvault/postgres.yaml
StandardOutput=journal
StandardError=journal
```

### Timer Unit (`/etc/systemd/system/dbvault.timer`)
```ini
[Unit]
Description=Trigger DBVault daily at 02:00 AM

[Timer]
OnCalendar=*-*-* 02:00:00
Persistent=true

[Install]
WantedBy=timers.target
```

Enable and start the timer:
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now dbvault.timer
```

---

## 4. Kubernetes `CronJob`

For containerized cloud environments:

```yaml
apiVersion: batch/v1
kind: CronJob
metadata:
  name: dbvault-daily-backup
  namespace: database
spec:
  schedule: "0 2 * * *"
  successfulJobsHistoryLimit: 3
  failedJobsHistoryLimit: 5
  concurrencyPolicy: Forbid
  jobTemplate:
    spec:
      template:
        spec:
          restartPolicy: OnFailure
          containers:
          - name: dbvault
            image: dbvault/dbvault:latest
            args:
            - "backup"
            - "--config"
            - "/etc/dbvault/config.yaml"
            env:
            - name: DB_PASSWORD
              valueFrom:
                secretKeyRef:
                  name: db-credentials
                  key: password
            volumeMounts:
            - name: config-volume
              mountPath: /etc/dbvault
          volumes:
          - name: config-volume
            configMap:
              name: dbvault-config
```

---

## 5. Concurrency Control and Locking

To prevent overlapping backup runs when backing up large datasets, ensure concurrency protection:
- **Kubernetes:** Set `concurrencyPolicy: Forbid`.
- **Linux cron:** Use `flock`:
  ```bash
  0 2 * * * /usr/bin/flock -n /var/lock/dbvault.lock /usr/local/bin/dbvault backup --config /etc/dbvault/postgres.yaml
  ```
- **In-process daemon:** One gate skips overlapping jobs within that daemon.
  Use a singleton supervisor or external locking when running multiple daemons.

## 6. Foreground daemon and persistent definitions

```bash
dbvault schedule --config configs/example.yaml --cron 'CRON_TZ=Asia/Bangkok 0 2 * * *'
dbvault schedule add --id daily --config configs/example.yaml --cron '0 2 * * *'
dbvault schedule list
dbvault schedule disable --id daily
dbvault schedule enable --id daily
dbvault schedule remove --id daily
dbvault schedule --state /etc/dbvault/schedules.json
```

The parser uses five fields; seconds and shorthand descriptors are unsupported.
UTC is the default timezone. The daemon loads enabled definitions once at
startup; restart it after edits. Definitions store absolute config paths and
reload configuration/environment credentials each run. Each backup has a
two-hour timeout. SIGINT/SIGTERM cancels the active backup and waits for cleanup.
There is no missed-run replay or immediate retry; the next cron time triggers
another attempt. State writes use an exclusive `.lock` file and atomic
replacement. If a writer crashes, remove its stale lock only after verifying
no writer is active. No credential values are persisted.
