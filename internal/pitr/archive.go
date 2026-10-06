// Package pitr keeps engine-native baselines and immutable log ranges separate
// from database-scoped logical dump backups.
package pitr

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/encryption"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/keymanager"
	"github.com/sung2708/DBVault/internal/pipeline"
	"github.com/sung2708/DBVault/internal/storage"
)

type Record struct {
	Version       int       `json:"version"`
	Name          string    `json:"name"`
	Engine        string    `json:"engine"`
	Identity      string    `json:"identity"`
	ServerVersion string    `json:"server_version"`
	GTIDMode      string    `json:"gtid_mode,omitempty"`
	Kind          string    `json:"kind"`
	Parent        string    `json:"parent,omitempty"`
	ParentHash    string    `json:"parent_hash,omitempty"`
	Start         string    `json:"start"`
	End           string    `json:"end"`
	SegmentSize   int64     `json:"segment_size,omitempty"`
	From          time.Time `json:"from"`
	Until         time.Time `json:"until"`
	Hash          string    `json:"sha256"`
	Size          int64     `json:"bytes"`
	KeyID         string    `json:"key_id,omitempty"`
	Algorithm     string    `json:"algorithm,omitempty"`
}

type Service struct {
	Config   config.Config
	Store    storage.Provider
	Runner   runner.Runner
	Password string
}

var objectName = regexp.MustCompile(`^pitr_[a-f0-9]{32}\.tar(?:\.enc)?$`)

func binding(m Record) []byte { m.Hash = ""; m.Size = 0; b, _ := json.Marshal(m); return b }
func (m Record) validate() error {
	if m.Version != 1 || !objectName.MatchString(m.Name) || m.Identity == "" || m.ServerVersion == "" || m.From.IsZero() || m.Until.Before(m.From) || m.Size <= 0 || m.Size > 1<<50 || (m.Engine != "postgres" && m.Engine != "mysql" && m.Engine != "mongodb") || (m.Kind != "base" && m.Kind != "logs") {
		return fmt.Errorf("invalid PITR record")
	}
	h, e := hex.DecodeString(m.Hash)
	if e != nil || len(h) != 32 {
		return fmt.Errorf("invalid PITR checksum")
	}
	if m.Kind == "base" && m.Parent != "" {
		return fmt.Errorf("PITR base has a parent")
	}
	if m.Kind == "logs" {
		h, e = hex.DecodeString(m.ParentHash)
		if !objectName.MatchString(m.Parent) || e != nil || len(h) != 32 || m.Parent == m.Name {
			return fmt.Errorf("invalid PITR parent")
		}
	}
	if m.KeyID != "" && m.Algorithm != encryption.Algorithm && m.Algorithm != encryption.ManagedAlgorithm {
		return fmt.Errorf("unsupported PITR encryption")
	}
	if (m.KeyID == "") != (m.Algorithm == "") {
		return fmt.Errorf("invalid PITR encryption reference")
	}
	if len(m.KeyID) > 128 || len(m.Identity) > 512 || len(m.ServerVersion) > 128 {
		return fmt.Errorf("oversized PITR identity")
	}
	switch m.Engine {
	case "postgres":
		if nextWAL(m.Start, m.SegmentSize) == "" || nextWAL(m.End, m.SegmentSize) == "" || m.Start[:8] != m.End[:8] || m.End < m.Start {
			return fmt.Errorf("invalid WAL range")
		}
	case "mysql":
		if m.GTIDMode != "" && m.GTIDMode != "ON" && m.GTIDMode != "OFF" {
			return fmt.Errorf("native PITR requires stable MySQL GTID mode ON or OFF")
		}
		for _, cursor := range []string{m.Start, m.End} {
			name, pos, ok := strings.Cut(cursor, ":")
			n, err := strconv.ParseUint(pos, 10, 64)
			if !ok || !binlogName.MatchString(name) || err != nil || n < 4 {
				return fmt.Errorf("invalid binlog range")
			}
		}
	case "mongodb":
		start, err := parseStamp(m.Start)
		end, e := parseStamp(m.End)
		if err != nil || e != nil || start.T == 0 || end.T < start.T || (end.T == start.T && end.I < start.I) || len(strings.Split(m.Identity, "/")) != 3 {
			return fmt.Errorf("invalid oplog range")
		}
	}
	return nil
}

func (s *Service) Read(ctx context.Context, name string) (Record, error) {
	var m Record
	if !objectName.MatchString(name) {
		return m, fmt.Errorf("invalid PITR object name")
	}
	r, e := s.Store.Get(ctx, name+".pitr.json")
	if e != nil {
		return m, e
	}
	defer r.Close()
	b, e := io.ReadAll(io.LimitReader(r, (64<<10)+1))
	if e != nil || len(b) > 64<<10 {
		return m, fmt.Errorf("invalid PITR record size")
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&m) != nil || m.Name != name {
		return m, fmt.Errorf("invalid PITR record")
	}
	if d.Decode(new(any)) != io.EOF {
		return m, fmt.Errorf("trailing PITR record data")
	}
	if e = m.validate(); e != nil {
		return m, e
	}
	return m, nil
}

// Native objects use a separate namespace. Logical retention/deletion cannot
// accidentally remove log ranges or the physical base they depend on.
func (s *Service) List(ctx context.Context) ([]Record, error) {
	objects, e := s.Store.List(ctx, "pitr_")
	if e != nil {
		return nil, e
	}
	records := []Record{}
	for _, o := range objects {
		if strings.HasSuffix(o.Key, ".pitr.json") {
			m, e := s.Read(ctx, strings.TrimSuffix(o.Key, ".pitr.json"))
			if e != nil {
				return nil, e
			}
			if m.Engine == s.Config.Database.Type {
				records = append(records, m)
			}
		}
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Until.Equal(records[j].Until) {
			return records[i].Name < records[j].Name
		}
		return records[i].Until.After(records[j].Until)
	})
	return records, nil
}

func (s *Service) publish(ctx context.Context, m Record, dir string) (Record, error) {
	id := make([]byte, 16)
	if _, e := rand.Read(id); e != nil {
		return m, e
	}
	m.Version = 1
	m.Name = "pitr_" + hex.EncodeToString(id) + ".tar"
	var key []byte
	var wrapper encryption.Wrapper
	var err error
	if c := s.Config.Encryption; c != nil {
		m.KeyID = c.KeyID
		m.Name += ".enc"
		if c.Providers[c.KeyID].Type != "" {
			m.Algorithm = encryption.ManagedAlgorithm
			wrapper, err = keymanager.Resolve(ctx, c, c.KeyID)
		} else {
			m.Algorithm = encryption.Algorithm
			key, err = encryption.Key(c.Keys[c.KeyID])
		}
		if err != nil {
			return m, err
		}
		defer clear(key)
	}
	f, e := os.CreateTemp("", "dbvault-pitr-publish-*")
	if e != nil {
		return m, e
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	var dst io.Writer = f
	var encrypted *encryption.Writer
	if wrapper != nil {
		encrypted, e = encryption.NewManagedWriter(ctx, f, wrapper, binding(m))
	} else if key != nil {
		encrypted, e = encryption.NewWriter(f, key, binding(m))
	}
	if e != nil {
		return m, e
	}
	if encrypted != nil {
		dst = encrypted
	}
	tw := tar.NewWriter(dst)
	e = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if path == dir {
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("native backups with external tablespaces/symlinks are not supported")
		}
		if d.IsDir() {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			return tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0700, Typeflag: tar.TypeDir})
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("nonregular native backup file")
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if err = tw.WriteHeader(&tar.Header{Name: filepath.ToSlash(rel), Mode: 0600, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		n, err := io.Copy(tw, pipeline.Reader{Context: ctx, Source: src})
		ce := src.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
		if n != info.Size() {
			return fmt.Errorf("native file changed while archiving")
		}
		return nil
	})
	if e != nil {
		return m, e
	}
	if e = tw.Close(); e != nil {
		return m, e
	}
	if encrypted != nil {
		if e = encrypted.Close(); e != nil {
			return m, e
		}
	}
	if _, e = f.Seek(0, 0); e != nil {
		return m, e
	}
	h := sha256.New()
	m.Size, e = io.Copy(h, f)
	if e != nil {
		return m, e
	}
	m.Hash = hex.EncodeToString(h.Sum(nil))
	if e = m.validate(); e != nil {
		return m, e
	}
	f.Seek(0, 0)
	if e = s.Store.Put(ctx, m.Name, f, m.Size); e != nil {
		return m, e
	}
	b, _ := json.Marshal(m)
	if e = s.Store.Put(ctx, m.Name+".pitr.json", bytes.NewReader(b), int64(len(b))); e != nil {
		c, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		s.Store.Delete(c, m.Name)
		return m, e
	}
	return m, nil
}

func (s *Service) unpack(ctx context.Context, m Record, dir string) error {
	r, e := s.Store.Get(ctx, m.Name)
	if e != nil {
		return e
	}
	defer r.Close()
	f, e := os.CreateTemp("", "dbvault-pitr-verify-*")
	if e != nil {
		return e
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	h := sha256.New()
	n, e := io.Copy(io.MultiWriter(f, h), io.LimitReader(pipeline.Reader{Context: ctx, Source: r}, m.Size+1))
	if e != nil {
		return e
	}
	if n != m.Size || hex.EncodeToString(h.Sum(nil)) != m.Hash {
		return fmt.Errorf("PITR archive checksum mismatch")
	}
	f.Seek(0, 0)
	var src io.Reader = pipeline.Reader{Context: ctx, Source: f}
	if m.KeyID != "" {
		var dec *encryption.Reader
		if m.Algorithm == encryption.ManagedAlgorithm {
			w, err := keymanager.Resolve(ctx, s.Config.Encryption, m.KeyID)
			if err != nil {
				return err
			}
			dec, e = encryption.NewManagedReader(ctx, src, w, binding(m))
		} else {
			if s.Config.Encryption == nil {
				return fmt.Errorf("PITR key mapping missing")
			}
			key, err := encryption.Key(s.Config.Encryption.Keys[m.KeyID])
			if err != nil {
				return err
			}
			defer clear(key)
			dec, e = encryption.NewReader(src, key, binding(m))
		}
		if e != nil {
			return e
		}
		// Authenticate the entire stream before extracting any files.
		plain, err := os.CreateTemp("", "dbvault-pitr-plain-*")
		if err != nil {
			return err
		}
		defer func() { plain.Close(); os.Remove(plain.Name()) }()
		if _, err = io.Copy(plain, dec); err != nil {
			return err
		}
		plain.Seek(0, 0)
		src = pipeline.Reader{Context: ctx, Source: plain}
	}
	tr := tar.NewReader(src)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if (header.Typeflag != tar.TypeReg && header.Typeflag != tar.TypeDir) || !filepath.IsLocal(header.Name) || strings.Contains(header.Name, "\\") || strings.Contains(header.Name, ":") {
			return fmt.Errorf("unsafe native archive member")
		}
		path := filepath.Join(dir, filepath.FromSlash(header.Name))
		if header.Typeflag == tar.TypeDir {
			if err = os.MkdirAll(path, 0700); err != nil {
				return err
			}
			continue
		}
		if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return err
		}
		out, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, err = io.Copy(out, tr)
		ce := out.Close()
		if err != nil {
			return err
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}

func (s *Service) chain(ctx context.Context, name string) ([]Record, error) {
	var records []Record
	seen := map[string]bool{}
	for len(records) < 1024 {
		if seen[name] {
			return nil, fmt.Errorf("cyclic PITR chain")
		}
		seen[name] = true
		m, e := s.Read(ctx, name)
		if e != nil {
			return nil, e
		}
		if len(records) > 0 {
			child := records[len(records)-1]
			if m.Hash != child.ParentHash || m.Identity != child.Identity || m.Engine != child.Engine || m.End != child.Start || !m.Until.Equal(child.From) || m.ServerVersion != child.ServerVersion || m.GTIDMode != child.GTIDMode || m.SegmentSize != child.SegmentSize {
				return nil, fmt.Errorf("PITR chain continuity mismatch")
			}
		}
		records = append(records, m)
		if m.Kind == "base" {
			for i, j := 0, len(records)-1; i < j; i, j = i+1, j-1 {
				records[i], records[j] = records[j], records[i]
			}
			return records, nil
		}
		name = m.Parent
	}
	return nil, fmt.Errorf("PITR chain too long; create a new base")
}
