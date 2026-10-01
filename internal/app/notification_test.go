package app

import (
	"context"
	"errors"
	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/notify"
	"testing"
)

type recordedNotifier struct{ events []notify.Event }

func (n *recordedNotifier) Send(ctx context.Context, e notify.Event) error {
	n.events = append(n.events, e)
	return errors.New("injected notification outage")
}
func TestNotificationOutageDoesNotAlterBackupRestore(t *testing.T) {
	svc, db, _ := setup(t, "gzip")
	svc.Config.Protection = &config.Protection{VerifyAfterBackup: true}
	n := &recordedNotifier{}
	svc.Notifier = n
	m, e := svc.Backup(context.Background(), "full", false)
	if e != nil {
		t.Fatal(e)
	}
	if e = svc.Restore(context.Background(), m.Name, true, false, database.RestoreOptions{}); e != nil {
		t.Fatal(e)
	}
	db.dumpErr = errors.New("injected dump failure")
	if _, e = svc.Backup(context.Background(), "full", false); e == nil {
		t.Fatal("dump failure lost")
	}
	if len(n.events) != 3 || n.events[0].Status != "completed" || n.events[0].BackupStatus != "success" || !n.events[0].VerificationRequested || n.events[0].VerificationStatus != "verified" || n.events[0].VerificationEvidenceStatus != "recorded" || n.events[1].Operation != "restore" || n.events[2].Status != "failed" || n.events[2].BackupStatus != "failed" {
		t.Fatal(n.events)
	}
	svc.Backup(context.Background(), "full", true)
	if len(n.events) != 3 {
		t.Fatal("dry run notified")
	}
}
