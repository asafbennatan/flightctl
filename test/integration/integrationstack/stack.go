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
)

const postgresInitSQL = `
CREATE USER flightctl_app WITH PASSWORD 'adminpass' CREATEDB;
CREATE USER flightctl_migrator WITH PASSWORD 'adminpass';
CREATE DATABASE flightctl OWNER flightctl_app;
GRANT ALL PRIVILEGES ON DATABASE flightctl TO flightctl_migrator;
`

const alertmanagerYAML = `
route:
  receiver: default
receivers:
  - name: default
`

// EnsureRunning starts Postgres, Redis, and Alertmanager with reuse if they are not already running.
func EnsureRunning(ctx context.Context) error {
	network := containers.GetDockerNetwork()
	reuse := true

	initDir, err := os.MkdirTemp("", "flightctl-integration-pg-init-*")
	if err != nil {
		return fmt.Errorf("temp dir for postgres init: %w", err)
	}
	defer func() { _ = os.RemoveAll(initDir) }()
	initPath := filepath.Join(initDir, "01-flightctl.sql")
	if err := os.WriteFile(initPath, []byte(postgresInitSQL), 0600); err != nil {
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
			"POSTGRES_PASSWORD": "adminpass",
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
		Cmd:          []string{"redis-server", "--requirepass", "adminpass"},
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
