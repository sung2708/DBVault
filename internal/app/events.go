package app

import (
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/metadata"
	"io"
)

// Event describes completed work or bytes consumed by an actual pipeline.
// Observers must be quick and safe for concurrent calls. They never control the
// operation and must not retain backup bytes. The core has no terminal dependency.
type Event struct {
	Stage, State string
	Bytes, Total int64
	Info         database.Info
	Manifest     metadata.Manifest
}

func (s *Service) emit(e Event) {
	if s.Observe != nil {
		s.Observe(e)
	}
}
func (s *Service) stage(stage, state string) { s.emit(Event{Stage: stage, State: state}) }

type meteredReader struct {
	io.Reader
	bytes, total int64
	stage        string
	service      *Service
}

func (r *meteredReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytes += int64(n)
	if n > 0 {
		r.service.emit(Event{Stage: r.stage, State: "progress", Bytes: r.bytes, Total: r.total})
	}
	return n, err
}
func (s *Service) meter(r io.Reader, stage string, total int64) io.Reader {
	if s.Observe == nil {
		return r
	}
	return &meteredReader{Reader: r, stage: stage, total: total, service: s}
}
