package presentation

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/doctor"
	"github.com/sung2708/DBVault/internal/metadata"
	"github.com/sung2708/DBVault/internal/onboarding"
	"github.com/sung2708/DBVault/internal/schedule"
	"github.com/sung2708/DBVault/internal/update"
)

func (r *Renderer) Result(operation string, value any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.options.JSON {
		return fmt.Errorf("human renderer cannot emit machine results")
	}
	var b strings.Builder
	switch v := value.(type) {
	case doctor.Report:
		title, state := "System readiness checks", "success"
		if !v.Ready {
			title, state = "Readiness checks need attention", "error"
		} else if v.Summary.Warnings > 0 {
			state = "warning"
		}
		r.title(&b, state, title)
		for _, check := range v.Checks {
			style := string(check.Status)
			if style == "pass" {
				style = "success"
			}
			if style == "skipped" {
				style = "header"
			}
			fmt.Fprintf(&b, "  %s %s\n", r.status(style, string(check.Status), false), r.safe(check.Name+" — "+check.Summary))
			if check.Detail != "" {
				r.field(&b, "Detail", check.Detail)
			}
			if check.Remediation != "" {
				r.hint(&b, check.Remediation)
			}
		}
		fmt.Fprintf(&b, "\n%d passed, %d warnings, %d failed, %d skipped\n", v.Summary.Passed, v.Summary.Warnings, v.Summary.Failed, v.Summary.Skipped)
		if v.Ready {
			r.hint(&b, "Ready for a backup preflight: dbvault backup --dry-run")
		}
	case update.Result:
		r.updateResult(&b, v)
	case onboarding.Result:
		r.title(&b, "success", "Configuration created")
		r.field(&b, "Config", v.Path)
		r.field(&b, "Engine", Engine(v.Database))
		r.field(&b, "Database", v.DatabaseName)
		r.field(&b, "Storage", v.Storage)
		r.field(&b, "Location", v.StorageLocation)
		r.field(&b, "Compression", v.Compression)
		status := "Not tested"
		if v.Tested {
			status = "Database connection and native tools verified"
		}
		r.field(&b, "Connection", status)
		if v.PasswordEnv != "" {
			r.hint(&b, "Before database operations, set environment variable: "+r.safe(v.PasswordEnv))
		}
		path := quoteSetupPath(r.safe(v.Path))
		r.hint(&b, fmt.Sprintf("Next:\n  dbvault config --config %s\n  dbvault test --config %s\n  dbvault doctor --config %s\n  dbvault backup --config %s --dry-run\n  dbvault backup --config %s", path, path, path, path, path))
	case metadata.Manifest:
		if r.options.Quiet && operation == "backup" {
			fmt.Fprintln(&b, r.safe(v.Name))
			break
		}
		title := "Backup completed"
		if operation == "verify" {
			title = "Stored size and SHA-256 verified"
		}
		if operation == "inspect" {
			title = "Backup metadata (archive bytes not verified)"
		}
		r.title(&b, "success", title)
		r.backupFields(&b, v, operation == "inspect")
		if operation == "backup" {
			r.hint(&b, "Next: use the Backup name with dbvault verify --target NAME.")
		}
	case []metadata.Manifest:
		if operation == "cleanup" {
			title := "Retention cleanup completed"
			if r.details.DryRun {
				title = "Cleanup preview: no backups deleted"
			}
			r.title(&b, "success", title)
			if len(v) == 0 {
				fmt.Fprintln(&b, "No retention candidates. No backups deleted.")
			} else {
				r.backupTable(&b, v)
				var total int64
				for _, m := range v {
					total += m.Pipeline.Stored
				}
				fmt.Fprintf(&b, "\n%d backups, %s total\n", len(v), FormatBytes(total))
			}
			if r.details.DryRun {
				r.hint(&b, "Dry run only. Review candidates before running cleanup without --dry-run.")
			}
		} else {
			if len(v) == 0 {
				fmt.Fprintln(&b, "No backups found.")
				r.hint(&b, "Create one with: dbvault backup")
			} else {
				r.backupTable(&b, v)
				r.hint(&b, "Use a backup name with --target in inspect, verify or restore.")
			}
		}
	case database.Info:
		r.title(&b, "success", "Database connection and tools verified")
		r.field(&b, "Engine", Engine(r.details.Config.Database.Type))
		r.field(&b, "Database", r.details.Config.Database.Database)
		if r.details.Config.Database.Type != "sqlite" {
			r.field(&b, "Host", r.details.Config.Database.Host)
			r.field(&b, "Port", fmt.Sprint(r.details.Config.Database.Port))
		}
		r.field(&b, "Server", ServerVersion(r.details.Config.Database.Type, v.ServerVersion))
		if v.ToolVersion != "" {
			r.field(&b, "Dump tool", strings.Split(v.ToolVersion, "\n")[0])
		}
		if v.RestoreToolVersion != "" {
			r.field(&b, "Restore tool", strings.Split(v.RestoreToolVersion, "\n")[0])
		}
		r.hint(&b, "Next: dbvault backup --dry-run")
	case schedule.State:
		if len(v.Jobs) == 0 {
			fmt.Fprintln(&b, "No backup schedules configured.")
			r.hint(&b, "Create one with: dbvault schedule add --help")
		} else {
			rows := make([][]string, 0, len(v.Jobs))
			for _, job := range v.Jobs {
				state := "disabled"
				if job.Enabled {
					state = "enabled"
				}
				rows = append(rows, []string{job.ID, state, job.Cron, job.Config})
			}
			r.table(&b, []string{"ID", "STATE", "CRON", "CONFIG"}, rows)
			r.hint(&b, "Run enabled jobs with: dbvault schedule (foreground; restart after edits).")
		}
	case map[string]string:
		if operation == "version" {
			r.title(&b, "header", "DBVault")
			for _, entry := range [][2]string{{"Version", v["version"]}, {"Commit", v["commit"]}, {"Built", v["built"]}, {"Go", v["runtime"]}, {"Platform", runtime.GOOS + "/" + runtime.GOARCH}} {
				r.field(&b, entry[0], entry[1])
			}
		} else {
			r.title(&b, "success", "Schedule "+v["action"]+" completed")
			r.field(&b, "Schedule ID", v["schedule"])
			r.hint(&b, "Start or restart the schedule daemon to apply saved definitions.")
		}
	case string:
		message := v
		switch operation {
		case "restore":
			message = "Restore completed"
			if r.details.DryRun {
				message = "Restore preview passed; no database writes"
			}
		case "delete":
			message = "Backup deleted"
			if r.details.DryRun {
				message = "Deletion preview; no backups deleted"
			}
		case "backup":
			if r.details.DryRun {
				message = "Backup preflight passed; no backup created"
			}
		}
		r.title(&b, "success", message)
		if operation == "config" {
			r.field(&b, "Config", r.details.ConfigPath)
			r.hint(&b, "Next: dbvault test")
		}
		if operation == "restore" || operation == "delete" {
			r.field(&b, "Backup name", r.details.Target)
		}
		if operation == "restore" {
			r.field(&b, "Database", r.details.Config.Database.Database)
		}
		if operation == "restore" && !r.details.Started.IsZero() {
			r.field(&b, "Duration", FormatDuration(r.options.Now().Sub(r.details.Started)))
		}
	default:
		return fmt.Errorf("unsupported presentation result %T", value)
	}
	_, err := fmt.Fprint(r.out, b.String())
	return err
}

// Quote for the documented Windows PowerShell and Unix shell workflows.
func quoteSetupPath(path string) string {
	if runtime.GOOS == "windows" {
		return `"` + strings.NewReplacer("`", "``", "$", "`$", `"`, "`\"").Replace(path) + `"`
	}
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func (r *Renderer) updateResult(b *strings.Builder, v update.Result) {
	r.title(b, "header", "DBVault Update")
	r.field(b, "Installed", v.InstalledVersion)
	if v.LatestVersion != "" {
		r.field(b, "Latest", v.LatestVersion)
	}
	switch v.State {
	case update.Development:
		r.title(b, "warning", "Development build detected")
		r.hint(b, "Automatic release comparison is unavailable for this build.")
	case update.UpdateAvailable:
		fmt.Fprintln(b)
		fmt.Fprintln(b, r.status("warning", "New version available", false))
		fmt.Fprintf(b, "\n  %s → %s\n", r.outStyle.muted.Render(r.safe(v.InstalledVersion)), r.outStyle.accent.Render(r.safe(v.LatestVersion)))
		r.hint(b, "Update with Go:\n\n  "+r.outStyle.accent.Render(r.safe(v.UpdateCommand)))
		if v.MajorUpgrade {
			r.hint(b, "This update changes the major version. Review the release notes and upgrade instructions before updating.")
		}
		r.hint(b, "Prebuilt binary:\n  "+r.outStyle.accent.Render(r.safe(v.ReleaseURL)))
	case update.UpToDate:
		r.title(b, "success", "You're running the latest stable release.")
	case update.InstalledNewer:
		r.title(b, "warning", "Installed version is newer than the latest published stable release.")
		r.hint(b, "No downgrade is suggested. See the official release page: "+r.safe(v.ReleaseURL))
	case update.VersionUnknown:
		r.title(b, "warning", "Installed version is not a valid SemVer release.")
		r.hint(b, "No update comparison or downgrade command is available. See the official release page: "+r.safe(v.ReleaseURL))
	}
}

func (r *Renderer) backupFields(b *strings.Builder, m metadata.Manifest, full bool) {
	for _, f := range [][2]string{{"Backup name", m.Name}, {"Backup ID", m.ID}, {"Database", m.Database.Name}, {"Engine", Engine(m.Database.Engine)}, {"Type", m.BackupType}, {"Compression", m.Pipeline.Compression}, {"Stored size", FormatBytes(m.Pipeline.Stored)}, {"Storage", m.Storage}, {"Checksum", FormatChecksum(m.Checksum.Hash, full)}, {"Duration", FormatDuration(time.Duration(m.Duration * float64(time.Second)))}, {"Created", FormatTime(m.CreatedAt)}} {
		r.field(b, f[0], f[1])
	}
	if full {
		for _, f := range [][2]string{{"Status", m.Status}, {"Manifest", m.Version}, {"Server", ServerVersion(m.Database.Engine, m.Database.Version)}, {"Format", m.Database.Format}, {"Raw size", FormatBytes(m.Pipeline.Raw)}, {"Completed", FormatTime(m.CompletedAt)}, {"Tool", strings.Split(m.ToolVersion, "\n")[0]}, {"DBVault", m.ApplicationVersion}} {
			r.field(b, f[0], f[1])
		}
		for _, f := range []struct {
			label  string
			values []string
		}{{"Tables", m.Database.IncludeTables}, {"Exclude tables", m.Database.ExcludeTables}, {"Collections", m.Database.IncludeCollections}, {"Exclude cols", m.Database.ExcludeCollections}} {
			if len(f.values) > 0 {
				r.field(b, f.label, strings.Join(f.values, ", "))
			}
		}
	}
}
func (r *Renderer) backupTable(b *strings.Builder, items []metadata.Manifest) {
	rows := make([][]string, 0, len(items))
	for _, m := range items {
		rows = append(rows, []string{m.Name, Engine(m.Database.Engine), m.BackupType, FormatBytes(m.Pipeline.Stored), m.CreatedAt.UTC().Format("2006-01-02 15:04Z")})
	}
	r.table(b, []string{"BACKUP NAME", "ENGINE", "TYPE", "SIZE", "CREATED (UTC)"}, rows)
}

// Wide terminals get borderless tables; narrow terminals get labeled records.
// Backup names and schedule paths are preserved rather than shortened identifiers.
func (r *Renderer) table(b *strings.Builder, headers []string, rows [][]string) {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = ansi.StringWidth(h)
	}
	for _, row := range rows {
		for i, s := range row {
			widths[i] = max(widths[i], ansi.StringWidth(r.safe(s)))
		}
	}
	total := 2 * (len(headers) - 1)
	for _, width := range widths {
		total += width
	}
	if total > r.outCaps.Width {
		for index, row := range rows {
			if index > 0 {
				fmt.Fprintln(b)
			}
			for i, value := range row {
				r.field(b, headers[i], value)
			}
		}
		return
	}
	writeRow := func(row []string, header bool) {
		for i, s := range row {
			s = r.safe(s)
			if header {
				s = r.outStyle.accent.Render(s)
			}
			fmt.Fprint(b, s)
			if i < len(row)-1 {
				fmt.Fprint(b, strings.Repeat(" ", widths[i]-ansi.StringWidth(s)+2))
			}
		}
		fmt.Fprintln(b)
	}
	writeRow(headers, true)
	for _, row := range rows {
		writeRow(row, false)
	}
}
