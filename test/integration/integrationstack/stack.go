// Package integrationstack starts or stops the named Postgres, Redis, and Alertmanager
// testcontainers used by integration tests. Host ports are assigned by the runtime (ephemeral);
// tests resolve them via podman/docker port (see internal/store/testutil/integration_ports.go).
// Container names must match internal/store/testutil constants.
package integrationstack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/flightctl/flightctl/test/harness/containers"
	"github.com/sirupsen/logrus"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// Names must match internal/store/testutil constants.
const (
	postgresContainerName     = "flightctl-integration-postgres"
	redisContainerName        = "flightctl-integration-redis"
	alertmanagerContainerName = "flightctl-integration-alertmanager"
)

const (
	postgresImage     = "docker.io/library/postgres:16-alpine"
	redisImage        = "docker.io/library/redis:7-alpine"
	alertmanagerImage = "docker.io/prom/alertmanager:v0.27.0"
	// defaultIntegrationPassword matches test/test.mk when integration env vars are unset (e.g. go run preflight alone).
	defaultIntegrationPassword = "adminpass"
)

const alertmanagerYAML = `
route:
  receiver: default
receivers:
  - name: default
`

func envOrDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func sqlStringLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// postgresInitSQLScript matches credentials from test/test.mk (FLIGHTCTL_POSTGRESQL_*).
func postgresInitSQLScript(appUserPassword, migratorPassword string) string {
	return fmt.Sprintf(`
CREATE USER flightctl_app WITH PASSWORD %s CREATEDB;
CREATE USER flightctl_migrator WITH PASSWORD %s;
CREATE DATABASE flightctl OWNER flightctl_app;
GRANT ALL PRIVILEGES ON DATABASE flightctl TO flightctl_migrator;
`, sqlStringLiteral(appUserPassword), sqlStringLiteral(migratorPassword))
}

func integrationStackAlreadyRunning() bool {
	for _, n := range []string{postgresContainerName, redisContainerName, alertmanagerContainerName} {
		if !containers.ContainerRunningByName(n) {
			return false
		}
	}
	return true
}

// EnsureRunning starts Postgres, Redis, and Alertmanager with reuse if they are not already running.
// If all three integration containers are already running (e.g. started by preflight or a prior test
// process), skips testcontainers the same way E2E aux checks containers.ContainerExistsByName before work.
func EnsureRunning(ctx context.Context) error {
	if integrationStackAlreadyRunning() {
		logrus.Info("Integration stack already running; skipping container start")
		return nil
	}

	network := containers.GetDockerNetwork()
	reuse := true

	initDir, err := os.MkdirTemp("", "flightctl-integration-pg-init-*")
	if err != nil {
		return fmt.Errorf("temp dir for postgres init: %w", err)
	}
	defer func() { _ = os.RemoveAll(initDir) }()
	appUserPW := envOrDefault("FLIGHTCTL_POSTGRESQL_USER_PASSWORD", defaultIntegrationPassword)
	migratorPW := envOrDefault("FLIGHTCTL_POSTGRESQL_MIGRATOR_PASSWORD", defaultIntegrationPassword)
	masterPW := envOrDefault("FLIGHTCTL_POSTGRESQL_MASTER_PASSWORD", defaultIntegrationPassword)
	kvPW := envOrDefault("FLIGHTCTL_KV_PASSWORD", defaultIntegrationPassword)

	initPath := filepath.Join(initDir, "01-flightctl.sql")
	if err := os.WriteFile(initPath, []byte(postgresInitSQLScript(appUserPW, migratorPW)), 0600); err != nil {
		return fmt.Errorf("write postgres init: %w", err)
	}

	amDir, err := os.MkdirTemp("", "flightctl-integration-am-*")
	if err != nil {
		return fmt.Errorf("temp dir for alertmanager: %w", err)
	}
	defer func() { _ = os.RemoveAll(amDir) }()
	amPath := filepath.Join(amDir, "alertmanager.yml")
	if err := os.WriteFile(amPath, []byte(alertmanagerYAML), 0600); err != nil {
		return fmt.Errorf("write alertmanager config: %w", err)
	}

	pgReq := testcontainers.ContainerRequest{
		Image:        postgresImage,
		Name:         postgresContainerName,
		ExposedPorts: []string{"5432/tcp"},
		Env: map[string]string{
			"POSTGRES_PASSWORD": masterPW,
		},
		Files: []testcontainers.ContainerFile{
			{HostFilePath: initPath, ContainerFilePath: "/docker-entrypoint-initdb.d/01-flightctl.sql", FileMode: 0644},
		},
		WaitingFor: wait.ForListeningPort("5432/tcp").WithStartupTimeout(120 * time.Second),
		SkipReaper: reuse,
	}
	if _, err := containers.GenericStart(ctx, pgReq, reuse, containers.WithNetwork(network), containers.WithHostAccess()); err != nil {
		return fmt.Errorf("postgres container: %w", err)
	}
	logrus.Info("Postgres integration container is up")

	redisReq := testcontainers.ContainerRequest{
		Image:        redisImage,
		Name:         redisContainerName,
		ExposedPorts: []string{"6379/tcp"},
		Cmd:          []string{"redis-server", "--requirepass", kvPW},
		WaitingFor:   wait.ForListeningPort("6379/tcp").WithStartupTimeout(60 * time.Second),
		SkipReaper:   reuse,
	}
	if _, err := containers.GenericStart(ctx, redisReq, reuse, containers.WithNetwork(network), containers.WithHostAccess()); err != nil {
		return fmt.Errorf("redis container: %w", err)
	}
	logrus.Info("Redis integration container is up")

	amReq := testcontainers.ContainerRequest{
		Image:        alertmanagerImage,
		Name:         alertmanagerContainerName,
		ExposedPorts: []string{"9093/tcp"},
		Cmd:          []string{"--config.file=/etc/alertmanager/alertmanager.yml", "--storage.path=/tmp/am"},
		Files: []testcontainers.ContainerFile{
			{HostFilePath: amPath, ContainerFilePath: "/etc/alertmanager/alertmanager.yml", FileMode: 0644},
		},
		WaitingFor: wait.ForHTTP("/-/ready").WithPort("9093/tcp").WithStartupTimeout(60 * time.Second),
		SkipReaper: reuse,
	}
	if _, err := containers.GenericStart(ctx, amReq, reuse, containers.WithNetwork(network), containers.WithHostAccess()); err != nil {
		return fmt.Errorf("alertmanager container: %w", err)
	}
	logrus.Info("Alertmanager integration container is up")
	return nil
}

// Stop removes integration containers by name (best effort for each).
func Stop(_ context.Context) error {
	for _, name := range []string{
		alertmanagerContainerName,
		redisContainerName,
		postgresContainerName,
	} {
		if err := containers.RemoveContainerByName(name); err != nil {
			logrus.Warnf("remove %s: %v", name, err)
		}
	}
	return nil
}
