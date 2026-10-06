package app

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

type operationRecord struct {
	Version     int       `json:"version"`
	Operation   string    `json:"operation"`
	Engine      string    `json:"engine"`
	Database    string    `json:"database"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	Success     bool      `json:"success"`
	Bytes       int64     `json:"bytes"`
}

func (s *Service) recordOperation(ctx context.Context, op string, start time.Time, size int64, operationErr error) {
	if s.Store == nil || !s.Config.Metrics.RecordOperations {
		return
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return
	}
	record := operationRecord{1, op, s.Config.Database.Type, s.Config.Database.Database, start, s.now(), operationErr == nil, size}
	data, err := json.Marshal(record)
	if err != nil {
		return
	}
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err = s.Store.Put(c, "operation_"+hex.EncodeToString(id)+".json", bytes.NewReader(data), int64(len(data))); err != nil && s.Logger != nil {
		s.Logger.Warn("operation metrics record unavailable", "operation", op)
	}
}

// Metrics reads durable operation records. Counters cover this feature's records,
// not historical runs made before records existed. Scrapes never open archives.
func (s *Service) Metrics(ctx context.Context) (string, error) {
	objects, err := s.Store.List(ctx, "")
	if err != nil {
		return "", err
	}
	type counts struct {
		success, failure int64
		duration         float64
		bytes            int64
	}
	totals := map[string]*counts{"backup": {}, "recovery": {}}
	for _, object := range objects {
		if !strings.HasPrefix(object.Key, "operation_") || !strings.HasSuffix(object.Key, ".json") {
			continue
		}
		r, err := s.Store.Get(ctx, object.Key)
		if err != nil {
			return "", err
		}
		data, err := io.ReadAll(io.LimitReader(r, (16<<10)+1))
		r.Close()
		if err != nil {
			return "", err
		}
		if len(data) > 16<<10 {
			return "", fmt.Errorf("oversized operation metrics record")
		}
		var record operationRecord
		if err = json.Unmarshal(data, &record); err != nil {
			return "", fmt.Errorf("invalid operation metrics record")
		}
		if record.Version != 1 || totals[record.Operation] == nil || record.CompletedAt.Before(record.StartedAt) || record.Bytes < 0 {
			return "", fmt.Errorf("invalid operation metrics fields")
		}
		if record.Engine != s.Config.Database.Type || record.Database != s.Config.Database.Database {
			continue
		}
		c := totals[record.Operation]
		if record.Success {
			c.success++
		} else {
			c.failure++
		}
		c.duration += record.CompletedAt.Sub(record.StartedAt).Seconds()
		c.bytes += record.Bytes
	}
	quote := func(v string) string {
		return `"` + strings.NewReplacer("\\", "\\\\", "\n", "\\n", "\"", "\\\"").Replace(v) + `"`
	}
	labels := `engine=` + quote(s.Config.Database.Type) + `,database=` + quote(s.Config.Database.Database)
	var out strings.Builder
	out.WriteString("# HELP dbvault_operations_total Durable operation outcomes since metrics recording was enabled.\n# TYPE dbvault_operations_total counter\n")
	for _, op := range []string{"backup", "recovery"} {
		c := totals[op]
		fmt.Fprintf(&out, "dbvault_operations_total{%s,operation=%q,status=\"success\"} %d\ndbvault_operations_total{%s,operation=%q,status=\"failure\"} %d\n", labels, op, c.success, labels, op, c.failure)
	}
	out.WriteString("# HELP dbvault_operation_duration_seconds_sum Total recorded operation duration.\n# TYPE dbvault_operation_duration_seconds_sum counter\n")
	for _, op := range []string{"backup", "recovery"} {
		fmt.Fprintf(&out, "dbvault_operation_duration_seconds_sum{%s,operation=%q} %g\n", labels, op, totals[op].duration)
	}
	out.WriteString("# HELP dbvault_backup_bytes_total Total stored bytes from recorded backup operations.\n# TYPE dbvault_backup_bytes_total counter\n")
	fmt.Fprintf(&out, "dbvault_backup_bytes_total{%s} %d\n", labels, totals["backup"].bytes)
	items, err := s.List(ctx, "")
	if err != nil {
		return "", err
	}
	out.WriteString("# HELP dbvault_last_backup_timestamp_seconds Latest completed backup start time.\n# TYPE dbvault_last_backup_timestamp_seconds gauge\n# HELP dbvault_last_backup_size_bytes Latest completed stored artifact size.\n# TYPE dbvault_last_backup_size_bytes gauge\n# HELP dbvault_backup_age_seconds Age of latest completed backup.\n# TYPE dbvault_backup_age_seconds gauge\n")
	for _, m := range items {
		if m.Database.Engine == s.Config.Database.Type && m.Database.Name == s.Config.Database.Database {
			fmt.Fprintf(&out, "dbvault_last_backup_timestamp_seconds{%s} %d\ndbvault_last_backup_size_bytes{%s} %d\ndbvault_backup_age_seconds{%s} %g\n", labels, m.CreatedAt.Unix(), labels, m.Pipeline.Stored, labels, max(0, s.now().Sub(m.CreatedAt).Seconds()))
			break
		}
	}
	return out.String(), nil
}
