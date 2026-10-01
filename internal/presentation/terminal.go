// Package presentation owns terminal rendering. Database and storage code never
// import it; JSON result encoding remains in the CLI boundary.
package presentation

import (
	"golang.org/x/term"
	"io"
	"os"
	"sync"
)

type Capabilities struct {
	TTY, Color, Unicode bool
	Width               int
}

func Detect(w io.Writer) Capabilities {
	c := Capabilities{Width: 80}
	if f, ok := w.(interface{ Fd() uintptr }); ok {
		fd := int(f.Fd())
		c.TTY = term.IsTerminal(fd)
		if width, _, err := term.GetSize(fd); err == nil && width > 0 {
			c.Width = width
		}
	}
	c.Color = c.TTY && os.Getenv("TERM") != "dumb"
	c.Unicode = c.Color
	return c
}

// LockedWriter lets scheduled jobs and their reports share streams safely.
// Fd preserves terminal detection through the wrapper.
type LockedWriter struct {
	Writer io.Writer
	Mutex  *sync.Mutex
}

func (w *LockedWriter) Write(p []byte) (int, error) {
	w.Mutex.Lock()
	defer w.Mutex.Unlock()
	return w.Writer.Write(p)
}
func (w *LockedWriter) Fd() uintptr {
	if f, ok := w.Writer.(interface{ Fd() uintptr }); ok {
		return f.Fd()
	}
	return ^uintptr(0)
}
