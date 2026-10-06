package app

import "time"

// StatusResult is a read model over health, validated backup metadata and
// advisory saved schedule definitions. It does not establish daemon liveness.
type StatusResult struct {
	Database          StatusDatabase   `json:"database"`
	Protection        string           `json:"protection"`
	ProtectionNote    string           `json:"protection_note"`
	VerifyAfterBackup bool             `json:"verify_after_backup"`
	BackupHealth      HealthStatus     `json:"backup_health"`
	BackupReason      string           `json:"backup_reason"`
	LastBackup        *StatusBackup    `json:"last_backup,omitempty"`
	Integrity         string           `json:"integrity"`
	LastVerifiedAt    *time.Time       `json:"last_verified_at,omitempty"`
	RecoveryDrill     StatusRecovery   `json:"recovery_drill"`
	Storage           StatusStorage    `json:"storage"`
	Schedules         []StatusSchedule `json:"schedules"`
	ScheduleNote      string           `json:"schedule_note,omitempty"`
	RecentBackups     []StatusBackup   `json:"recent_backups"`
	GeneratedAt       time.Time        `json:"generated_at"`
}

type StatusDatabase struct {
	Name   string `json:"name"`
	Engine string `json:"engine"`
}
type StatusStorage struct {
	Type string `json:"type"`
}
type StatusBackup struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	AgeSeconds  float64   `json:"age_seconds"`
	Type        string    `json:"type"`
	StoredBytes int64     `json:"stored_bytes"`
	Status      string    `json:"status"`
}
type StatusRecovery struct {
	Status      string     `json:"status"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Note        string     `json:"note,omitempty"`
}
type StatusSchedule struct {
	Operation string `json:"operation,omitempty"`
	ID        string `json:"id"`
	Cron      string `json:"cron"`
	Enabled   bool   `json:"enabled"`
}

// BuildStatus projects existing health evidence into an operator-facing view.
func (s *Service) BuildStatus(health HealthReport, schedules []HealthSchedule, scheduleNote string, at time.Time) StatusResult {
	if at.IsZero() {
		at = s.now()
	}
	now := at.UTC()
	result := StatusResult{
		Database:          StatusDatabase{Name: s.Config.Database.Database, Engine: s.Config.Database.Type},
		Protection:        "not_configured",
		ProtectionNote:    "No protection policy evaluator is configured",
		VerifyAfterBackup: s.Config.Protection != nil && s.Config.Protection.VerifyAfterBackup,
		BackupHealth:      health.Status,
		BackupReason:      "Health evidence unavailable",
		Integrity:         "unknown",
		RecoveryDrill:     StatusRecovery{Status: "never_tested"},
		Storage:           StatusStorage{Type: s.Config.Storage.Type},
		Schedules:         []StatusSchedule{}, RecentBackups: []StatusBackup{}, GeneratedAt: now,
		ScheduleNote: scheduleNote,
	}
	if len(health.Databases) > 0 {
		h := health.Databases[0]
		result.BackupReason = h.Reason
		result.Integrity = h.Integrity
		result.LastVerifiedAt = h.LastVerifiedAt
		if h.RecoveryNote != "" {
			result.RecoveryDrill = StatusRecovery{Status: "unknown", Note: h.RecoveryNote}
		} else if h.LastRecoveryDrillAt != nil {
			result.RecoveryDrill = StatusRecovery{Status: h.RestoreTest, CompletedAt: h.LastRecoveryDrillAt}
		}
		for _, backup := range h.RecentBackups {
			statusBackup := StatusBackup{ID: backup.ID, Name: backup.Name, CreatedAt: backup.CreatedAt, AgeSeconds: now.Sub(backup.CreatedAt).Seconds(), Type: backup.Type, StoredBytes: backup.StoredBytes, Status: backup.Status}
			result.RecentBackups = append(result.RecentBackups, statusBackup)
		}
	}
	for _, job := range schedules {
		result.Schedules = append(result.Schedules, StatusSchedule{ID: job.ID, Cron: job.Cron, Enabled: job.Enabled, Operation: job.Operation})
	}
	if len(result.RecentBackups) > 0 {
		last := result.RecentBackups[0]
		result.LastBackup = &last
	}
	return result
}
