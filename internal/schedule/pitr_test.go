package schedule

import (
	"path/filepath"
	"testing"
)

func TestNativeJobPersistenceAndValidation(t *testing.T) {
	job := Job{ID: "native", Cron: "*/5 * * * *", Config: "native.yaml", Enabled: true, Operation: "pitr", BackupType: "incremental", BaseEvery: "24h", Cleanup: true}
	path := filepath.Join(t.TempDir(), "jobs.json")
	if err := Update(path, func(s *State) error { s.Jobs = append(s.Jobs, job); return nil }); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil || len(s.Jobs) != 1 || s.Jobs[0] != job {
		t.Fatal(s, err)
	}
	for _, mutate := range []func(*Job){func(j *Job) { j.BaseEvery = "-1h" }, func(j *Job) { j.BaseEvery = "daily" }, func(j *Job) { j.Operation = "backup" }, func(j *Job) { j.RecoveryDirectory = "target" }, func(j *Job) { j.BackupType = "delta" }} {
		bad := job
		mutate(&bad)
		if Validate(bad) == nil {
			t.Fatal("invalid native job accepted", bad)
		}
	}
}
