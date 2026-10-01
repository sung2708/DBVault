// Package toolresolve locates only supported native database tools using
// configured paths, PATH, and bounded platform installation locations.
package toolresolve

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/sung2708/DBVault/internal/fault"
)

type Lookup interface{ LookPath(string) (string, error) }

var required = map[string][]string{
	"postgres": {"pg_dump", "pg_restore", "psql"},
	"mysql":    {"mysqldump", "mysql"},
	"mongodb":  {"mongodump", "mongorestore"},
}

// Resolve observes explicit configuration first. Existing configurations retain
// PATH behavior, then use known vendor/distro locations without broad scanning.
func Resolve(name, explicit string, lookup Lookup) (string, error) {
	if explicit != "" {
		p, err := validate(explicit)
		if err != nil {
			return "", fault.Wrap(fault.Dependency, "resolve "+name, fmt.Errorf("configured executable is unavailable at %s; run dbvault doctor or re-run dbvault init: %w", explicit, err))
		}
		return p, nil
	}
	if lookup == nil {
		lookup = osLookup{}
	}
	if p, err := lookup.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range candidateDirs(runtime.GOOS, name) {
		p := filepath.Join(dir, executable(name, runtime.GOOS))
		if _, err := validate(p); err == nil {
			return p, nil
		}
	}
	return "", fault.Wrap(fault.Dependency, "resolve "+name, fmt.Errorf("native tool was not found in PATH or known installation locations"))
}

// Discover returns a complete toolchain from one directory so commands cannot
// accidentally combine tools from different installations.
func Discover(engine string) (map[string]string, error) {
	names, ok := required[engine]
	if !ok {
		return nil, nil
	}
	var dirs []string
	for _, name := range names {
		if p, err := exec.LookPath(name); err == nil {
			dirs = appendUnique(dirs, filepath.Dir(p))
		}
	}
	for _, name := range names {
		for _, d := range candidateDirs(runtime.GOOS, name) {
			dirs = appendUnique(dirs, d)
		}
	}
	for _, dir := range dirs {
		set := make(map[string]string, len(names))
		good := true
		for _, name := range names {
			p, err := validate(filepath.Join(dir, executable(name, runtime.GOOS)))
			if err != nil {
				good = false
				break
			}
			set[name] = p
		}
		if good {
			return set, nil
		}
	}
	return nil, fmt.Errorf("complete %s native toolchain was not found; required tools: %s", engine, strings.Join(names, ", "))
}

// DiscoverInDirectory validates the complete engine toolset in a user-selected
// directory. It never searches beneath that directory.
func DiscoverInDirectory(engine, dir string) (map[string]string, error) {
	names, ok := required[engine]
	if !ok {
		return nil, nil
	}
	if strings.TrimSpace(dir) == "" || strings.ContainsAny(dir, "\x00\r\n") {
		return nil, fmt.Errorf("native tool directory is invalid")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	if !st.IsDir() {
		return nil, fmt.Errorf("native tool path is not a directory")
	}
	set := make(map[string]string, len(names))
	for _, name := range names {
		p, e := validate(filepath.Join(abs, executable(name, runtime.GOOS)))
		if e != nil {
			return nil, fmt.Errorf("%s: %w", name, e)
		}
		set[name] = p
	}
	return set, nil
}

// SameToolchain rejects tool sets assembled from multiple installation dirs.
func SameToolchain(paths []string) error {
	if len(paths) < 2 {
		return nil
	}
	first := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		d := filepath.Dir(p)
		equal := filepath.Clean(first) == filepath.Clean(d)
		if runtime.GOOS == "windows" {
			equal = strings.EqualFold(filepath.Clean(first), filepath.Clean(d))
		}
		if !equal {
			return fmt.Errorf("native tools resolve to different installation directories")
		}
	}
	return nil
}

// ResolveAll applies one authoritative resolution policy to the engine's full
// required set and enforces that the results come from a single installation.
func ResolveAll(engine string, configured map[string]string, lookup Lookup) (map[string]string, error) {
	names, ok := required[engine]
	if !ok {
		return nil, nil
	}
	set := make(map[string]string, len(names))
	paths := make([]string, 0, len(names))
	for _, name := range names {
		p, err := Resolve(name, configured[name], lookup)
		if err != nil {
			return nil, err
		}
		set[name] = p
		paths = append(paths, p)
	}
	if err := SameToolchain(paths); err != nil {
		return nil, err
	}
	return set, nil
}

type osLookup struct{}

func (osLookup) LookPath(s string) (string, error) { return exec.LookPath(s) }

func executable(name, goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}
func validate(path string) (string, error) {
	if strings.ContainsAny(path, "\x00\r\n") {
		return "", fmt.Errorf("invalid native tool path")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", fmt.Errorf("native tool is not a regular file")
	}
	if runtime.GOOS != "windows" && st.Mode().Perm()&0111 == 0 {
		return "", fmt.Errorf("native tool is not executable")
	}
	return abs, nil
}
func appendUnique(a []string, s string) []string {
	for _, v := range a {
		if same(v, s) {
			return a
		}
	}
	return append(a, s)
}
func same(a, b string) bool {
	a, _ = filepath.Abs(a)
	b, _ = filepath.Abs(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

func candidateDirs(goos, tool string) []string {
	var dirs []string
	add := func(s string) {
		if s != "" {
			dirs = appendUnique(dirs, s)
		}
	}
	switch goos {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		if pf == "" {
			pf = `C:\Program Files`
		}
		pfx := os.Getenv("ProgramFiles(x86)")
		roots := []string{filepath.Join(pf, "PostgreSQL"), filepath.Join(pfx, "PostgreSQL")}
		switch tool {
		case "mysqldump", "mysql":
			roots = []string{filepath.Join(pf, "MySQL"), filepath.Join(pfx, "MySQL")}
		case "mongodump", "mongorestore":
			roots = []string{filepath.Join(pf, "MongoDB", "Tools"), filepath.Join(pfx, "MongoDB", "Tools")}
		}
		for _, root := range roots {
			entries, _ := os.ReadDir(root)
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}
			sort.SliceStable(names, func(i, j int) bool { return versionNumber(names[i]) > versionNumber(names[j]) })
			for _, n := range names {
				add(filepath.Join(root, n, "bin"))
			}
		}
	case "linux":
		add("/usr/bin")
		add("/usr/local/bin")
		if tool == "pg_dump" || tool == "pg_restore" || tool == "psql" {
			entries, _ := os.ReadDir("/usr/lib/postgresql")
			names := make([]string, 0, len(entries))
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, e.Name())
				}
			}
			sort.SliceStable(names, func(i, j int) bool { return versionNumber(names[i]) > versionNumber(names[j]) })
			for _, n := range names {
				add(filepath.Join("/usr/lib/postgresql", n, "bin"))
			}
		}
	case "darwin":
		switch tool {
		case "pg_dump", "pg_restore", "psql":
			add("/opt/homebrew/opt/libpq/bin")
			add("/usr/local/opt/libpq/bin")
			add("/Applications/Postgres.app/Contents/Versions/latest/bin")
		case "mysqldump", "mysql":
			add("/opt/homebrew/opt/mysql-client/bin")
			add("/usr/local/opt/mysql-client/bin")
		case "mongodump", "mongorestore":
			add("/opt/homebrew/opt/mongodb-database-tools/bin")
			add("/usr/local/opt/mongodb-database-tools/bin")
		}
	}
	return dirs
}

func versionNumber(s string) int {
	n := 0
	found := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			found = true
			n = n*10 + int(r-'0')
		} else if found {
			break
		}
	}
	return n
}
