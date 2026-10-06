package metadata

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
)

type Database struct {
	Engine             string   `json:"engine"`
	Version            string   `json:"version"`
	Name               string   `json:"database_name"`
	Format             string   `json:"format"`
	IncludeTables      []string `json:"include_tables,omitempty"`
	ExcludeTables      []string `json:"exclude_tables,omitempty"`
	IncludeCollections []string `json:"include_collections,omitempty"`
	ExcludeCollections []string `json:"exclude_collections,omitempty"`
}
type Pipeline struct {
	Compression string `json:"compression"`
	Level       int    `json:"compression_level"`
	Raw         int64  `json:"uncompressed_bytes"`
	Stored      int64  `json:"compressed_bytes"`
}
type Checksum struct {
	Algorithm string `json:"algorithm"`
	Hash      string `json:"hash"`
}
type Encryption struct {
	Algorithm string `json:"algorithm"`
	KeyID     string `json:"key_id"`
}
type Delta struct {
	BaseName string `json:"base_name"`
	BaseID   string `json:"base_id"`
	BaseHash string `json:"base_sha256"`
	RawHash  string `json:"raw_sha256"`
	Depth    int    `json:"depth"`
}

type Manifest struct {
	Encryption         *Encryption `json:"encryption,omitempty"`
	Delta              *Delta      `json:"delta,omitempty"`
	Version            string      `json:"manifest_version"`
	ID                 string      `json:"backup_id"`
	Name               string      `json:"backup_name"`
	BackupType         string      `json:"backup_type"`
	CreatedAt          time.Time   `json:"created_at"`
	CompletedAt        time.Time   `json:"completed_at"`
	Database           Database    `json:"database"`
	Pipeline           Pipeline    `json:"pipeline"`
	Checksum           Checksum    `json:"checksum"`
	Duration           float64     `json:"duration_seconds"`
	Status             string      `json:"status"`
	ApplicationVersion string      `json:"application_version"`
	ToolVersion        string      `json:"database_tool_version"`
	Storage            string      `json:"storage_provider"`
}

var unsafeName = regexp.MustCompile(`[^a-zA-Z0-9_-]+`)

func Identity(db, extension string, now time.Time) (string, string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	id := hex.EncodeToString(b)
	name := strings.Trim(unsafeName.ReplaceAllString(db, "_"), "_")
	if name == "" {
		name = "database"
	}
	if len(name) > 64 {
		name = name[:64]
	}
	return id, fmt.Sprintf("%s_%s_%s%s", name, now.UTC().Format("20060102_150405"), id, extension), nil
}
func (m Manifest) Validate() error {
	if m.Version != "1.0" {
		return fmt.Errorf("unsupported manifest_version")
	}
	if m.Status != "completed" || m.Name == "" || m.ID == "" || (m.BackupType != "full" && m.BackupType != "incremental") {
		return fmt.Errorf("invalid completed backup manifest")
	}
	if m.Encryption != nil && ((m.Encryption.Algorithm != "aes256-gcm-stream-v1" && m.Encryption.Algorithm != "aes256-gcm-stream-v2") || m.Encryption.KeyID == "" || len(m.Encryption.KeyID) > 128) {
		return fmt.Errorf("invalid encryption metadata")
	}
	if m.BackupType == "incremental" {
		if m.Delta == nil || m.Delta.BaseName == m.Name || m.Delta.BaseName == "" || strings.ContainsAny(m.Delta.BaseName, "/\\") || m.Delta.BaseID == "" || m.Delta.Depth < 1 || m.Delta.Depth > 32 {
			return fmt.Errorf("invalid incremental base")
		}
		for _, h := range []string{m.Delta.BaseHash, m.Delta.RawHash} {
			v, e := hex.DecodeString(h)
			if e != nil || len(v) != 32 {
				return fmt.Errorf("invalid incremental hash")
			}
		}
	} else if m.Delta != nil {
		return fmt.Errorf("full backup cannot reference a base")
	}
	if m.Database.Name == "" {
		return fmt.Errorf("missing database_name")
	}
	if m.CreatedAt.IsZero() || m.CompletedAt.Before(m.CreatedAt) || m.Pipeline.Stored <= 0 || m.Pipeline.Raw <= 0 || m.Duration < 0 {
		return fmt.Errorf("invalid manifest timing or size")
	}
	if m.Checksum.Algorithm != "sha256" {
		return fmt.Errorf("unsupported checksum algorithm")
	}
	v, err := hex.DecodeString(m.Checksum.Hash)
	if err != nil || len(v) != 32 {
		return fmt.Errorf("invalid SHA-256 digest")
	}
	switch m.Database.Engine {
	case "postgres":
		if m.Database.Format != "custom" {
			return fmt.Errorf("unsupported PostgreSQL format")
		}
	case "mysql":
		if m.Database.Format != "sql" {
			return fmt.Errorf("unsupported MySQL format")
		}
	case "mongodb":
		if strings.ContainsAny(m.Database.Name, "/\\.\"$ *?[]\x00\r\n") {
			return fmt.Errorf("invalid MongoDB source namespace")
		}
		if m.Database.Format != "archive" {
			return fmt.Errorf("unsupported MongoDB format")
		}
	case "sqlite":
		if m.Database.Format != "sqlite" {
			return fmt.Errorf("unsupported SQLite format")
		}
	default:
		return fmt.Errorf("unsupported database engine")
	}
	switch m.Pipeline.Compression {
	case "none", "gzip", "zstd":
	default:
		return fmt.Errorf("unsupported compression")
	}
	if m.Pipeline.Level < 1 || m.Pipeline.Level > 9 {
		return fmt.Errorf("invalid compression level")
	}
	return nil
}
func Decode(r io.Reader) (Manifest, error) {
	var m Manifest
	data, err := io.ReadAll(io.LimitReader(r, (1<<20)+1))
	if err != nil {
		return m, fmt.Errorf("read metadata: %w", err)
	}
	if len(data) > 1<<20 {
		return m, fmt.Errorf("metadata exceeds 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(&m); err != nil {
		return m, fmt.Errorf("invalid metadata JSON")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return m, fmt.Errorf("trailing metadata JSON")
	}
	return m, m.Validate()
}
func Encode(m Manifest) ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return json.MarshalIndent(m, "", "  ")
}
