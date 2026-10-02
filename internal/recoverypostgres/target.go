// Package recoverypostgres confines recovery to a newly created Docker server.
// The Docker daemon and preloaded official image are operator-trusted.
package recoverypostgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sung2708/DBVault/internal/config"
	"github.com/sung2708/DBVault/internal/database"
	"github.com/sung2708/DBVault/internal/database/postgres"
	runner "github.com/sung2708/DBVault/internal/exec"
	"github.com/sung2708/DBVault/internal/security"
)

const ValidationMethod = "PostgreSQL read-only catalog and table readability checks"

var databaseName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,62}$`)
var dockerID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Target struct {
	*postgres.Adapter
	native         runner.Runner
	docker         string
	token, imageID string
	ID             string
	Image          string
	major          int
}

func New(name, source string, native runner.Runner) (*Target, error) {
	if !databaseName.MatchString(name) || name == source || name == "postgres" || name == "template0" || name == "template1" {
		return nil, fmt.Errorf("recovery database must be a new lowercase PostgreSQL name (1-63 characters), different from the source and system databases")
	}
	secret := make([]byte, 48)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	password := hex.EncodeToString(secret[:32])
	if native == nil {
		native = runner.Native{Redactor: security.New(password)}
	}
	t := &Target{native: native, token: hex.EncodeToString(secret[32:])}
	t.Adapter = &postgres.Adapter{Config: config.Database{Type: "postgres", Host: "127.0.0.1", Port: 5432, User: "postgres", Database: name, SSLMode: "disable"}, Password: password, Runner: &containerRunner{target: t}}
	return t, nil
}

func (t *Target) capture(ctx context.Context, args ...string) (string, error) {
	var b limitedOutput
	err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: args, Stdout: &b})
	return strings.TrimSpace(b.String()), err
}

type limitedOutput struct{ bytes.Buffer }

func (b *limitedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 1<<20 {
		return 0, fmt.Errorf("Docker diagnostic output exceeds limit")
	}
	return b.Buffer.Write(p)
}

// CheckImage never pulls images or creates a container, including in dry-run.
func (t *Target) CheckImage(ctx context.Context, version, toolVersion string) error {
	n, err := strconv.Atoi(version)
	if err != nil || n < 100000 {
		return fmt.Errorf("invalid source PostgreSQL version")
	}
	t.major = n / 10000
	dumpMajor, err := postgres.Major(toolVersion)
	if err != nil || dumpMajor != t.major {
		return fmt.Errorf("source PostgreSQL dump/server majors must match")
	}
	t.Image = fmt.Sprintf("postgres:%d-bookworm", t.major)
	t.docker, err = t.native.LookPath("docker")
	if err != nil {
		return err
	}
	id, err := t.capture(ctx, "image", "inspect", "--format={{.Id}}", t.Image)
	if err != nil {
		return fmt.Errorf("preload the trusted recovery image with docker pull %s: %w", t.Image, err)
	}
	if !strings.HasPrefix(id, "sha256:") || !dockerID.MatchString(strings.TrimPrefix(id, "sha256:")) {
		return fmt.Errorf("invalid Docker image identity")
	}
	t.imageID = id
	return nil
}

func (t *Target) Start(ctx context.Context) error {
	if t.imageID == "" {
		return fmt.Errorf("recovery image was not checked")
	}
	// A random owned name is reserved by create; all later operations use its ID.
	var b limitedOutput
	args := []string{"create", "--pull=never", "--name", "dbvault-recovery-" + t.token, "--label", "io.dbvault.recovery=" + t.token,
		"--network=none", "--security-opt=no-new-privileges", "--mount", "type=volume,destination=/var/lib/postgresql/data",
		"--env", "PGDATA=/var/lib/postgresql/data", "--env", "POSTGRES_PASSWORD", "--env", "POSTGRES_DB", t.imageID}
	err := t.native.Run(ctx, runner.Spec{Executable: t.docker, Args: args, Env: map[string]string{"POSTGRES_PASSWORD": t.Password, "POSTGRES_DB": t.Config.Database}, Stdout: &b})
	id := strings.TrimSpace(b.String())
	if dockerID.MatchString(id) {
		t.ID = id
	}
	if err != nil {
		// A cancelled CLI may have created the container before losing stdout.
		// Recover only an identity with this run's label and isolation contract.
		if t.ID == "" {
			recoverCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			id, inspectErr := t.capture(recoverCtx, "inspect", "--format={{.Id}}", "dbvault-recovery-"+t.token)
			if inspectErr == nil && dockerID.MatchString(id) {
				t.ID = id
				if checkErr := t.Check(recoverCtx); checkErr != nil {
					t.ID = ""
				}
			}
		}
		return err
	}
	if t.ID == "" {
		return fmt.Errorf("Docker did not return a valid recovery container ID; inspect dbvault-recovery-%s", t.token)
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	if _, err := t.capture(ctx, "start", t.ID); err != nil {
		return err
	}
	ready, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if info, err := t.Preflight(ready); err == nil {
			version, err := strconv.Atoi(info.ServerVersion)
			if err != nil || version/10000 != t.major {
				return fmt.Errorf("recovery image server major does not match the backup source")
			}
			return nil
		}
		select {
		case <-ready.Done():
			return ready.Err()
		case <-ticker.C:
		}
	}
}

func (t *Target) Check(ctx context.Context) error {
	if !dockerID.MatchString(t.ID) {
		return fmt.Errorf("recovery container was not created")
	}
	data, err := t.capture(ctx, "inspect", t.ID)
	if err != nil {
		return err
	}
	var entries []struct {
		ID, Image  string
		Config     struct{ Labels map[string]string }
		HostConfig struct {
			NetworkMode  string
			Privileged   bool
			Binds        []string
			VolumesFrom  []string
			PortBindings map[string]json.RawMessage
		}
		Mounts          []struct{ Type, Destination string }
		NetworkSettings struct{ Networks map[string]json.RawMessage }
	}
	if err := json.Unmarshal([]byte(data), &entries); err != nil || len(entries) != 1 {
		return fmt.Errorf("invalid recovery container inspection")
	}
	c := entries[0]
	if c.ID != t.ID || c.Image != t.imageID || c.Config.Labels["io.dbvault.recovery"] != t.token || c.HostConfig.NetworkMode != "none" || c.HostConfig.Privileged || len(c.HostConfig.Binds) != 0 || len(c.HostConfig.VolumesFrom) != 0 || len(c.HostConfig.PortBindings) != 0 {
		return fmt.Errorf("recovery container ownership or isolation changed")
	}
	for network := range c.NetworkSettings.Networks {
		if network != "none" {
			return fmt.Errorf("recovery container acquired an external network")
		}
	}
	for _, mount := range c.Mounts {
		if mount.Type != "volume" || (mount.Destination != "/var/lib/postgresql/data" && mount.Destination != "/var/lib/postgresql") {
			return fmt.Errorf("unexpected recovery container mount")
		}
	}
	return nil
}

func (t *Target) Cleanup(ctx context.Context) error {
	if err := t.Check(ctx); err != nil {
		return err
	}
	_, err := t.capture(ctx, "rm", "--force", "--volumes", t.ID)
	return err
}

// Stop retained targets so a cancelled Docker exec cannot keep restoring in the
// background. Removal remains an explicit success-only operation.
func (t *Target) Stop(ctx context.Context) error {
	if t.ID == "" {
		return nil
	}
	if err := t.Check(ctx); err != nil {
		return err
	}
	_, err := t.capture(ctx, "stop", "--time=5", t.ID)
	return err
}

func (t *Target) ValidateRecovery(ctx context.Context) (database.RecoveryValidation, error) {
	if err := t.Check(ctx); err != nil {
		return database.RecoveryValidation{}, err
	}
	// Read every ordinary table/materialized view. No expected application dataset
	// is available in a logical dump manifest, so do not claim semantic validation.
	sql := `BEGIN READ ONLY; DO $$ DECLARE r record; BEGIN FOR r IN SELECT n.nspname, c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE (c.relkind = 'r' OR (c.relkind = 'm' AND c.relispopulated)) AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' LOOP EXECUTE format('SELECT count(*) FROM %I.%I', r.nspname, r.relname); END LOOP; END $$; SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE c.relkind IN ('r','m') AND n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'; COMMIT;`
	var b limitedOutput
	err := t.Adapter.Runner.Run(ctx, runner.Spec{Executable: "psql", Args: []string{"--no-psqlrc", "--no-password", "--quiet", "--tuples-only", "--no-align", "--set=ON_ERROR_STOP=1", "--command=" + sql}, Env: map[string]string{"PGHOST": "127.0.0.1", "PGPORT": "5432", "PGUSER": "postgres", "PGDATABASE": t.Config.Database, "PGPASSWORD": t.Password, "PGSSLMODE": "disable", "PGOPTIONS": ""}, Stdout: &b})
	if err != nil {
		return database.RecoveryValidation{}, err
	}
	n, err := strconv.ParseInt(strings.TrimSpace(b.String()), 10, 64)
	if err != nil || n < 0 {
		return database.RecoveryValidation{}, fmt.Errorf("invalid PostgreSQL recovery validation result")
	}
	return database.RecoveryValidation{Method: ValidationMethod, Objects: n}, nil
}

type containerRunner struct{ target *Target }

func (r *containerRunner) LookPath(name string) (string, error) {
	if name != "pg_dump" && name != "pg_restore" && name != "psql" {
		return "", fmt.Errorf("unsupported recovery executable")
	}
	return name, nil
}
func (r *containerRunner) Run(ctx context.Context, s runner.Spec) error {
	t := r.target
	if err := t.Check(ctx); err != nil {
		return err
	}
	if _, err := r.LookPath(s.Executable); err != nil {
		return err
	}
	args := []string{"exec", "--interactive"}
	for key := range s.Env {
		args = append(args, "--env", key)
	}
	args = append(args, t.ID, "env", "-u", "PGSERVICE", "-u", "PGSERVICEFILE", "-u", "PGHOSTADDR", "-u", "PGPASSFILE", s.Executable)
	args = append(args, s.Args...)
	s.Executable, s.Args = t.docker, args
	err := t.native.Run(ctx, s)
	if err != nil {
		return security.New(t.Password).Error(err)
	}
	return err
}

var _ database.Adapter = (*Target)(nil)
var _ io.Writer = (*limitedOutput)(nil)
