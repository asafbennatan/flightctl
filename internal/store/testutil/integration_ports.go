package testutil

import (
	"context"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/domain"
	"github.com/sirupsen/logrus"
)

// Integration container names (must match test/integration/integrationstack).
const (
	IntegrationPostgresContainer     = "flightctl-integration-postgres"
	IntegrationRedisContainer        = "flightctl-integration-redis"
	IntegrationAlertmanagerContainer = "flightctl-integration-alertmanager"
)

// publishedTCPPort resolves the host-published TCP port for a named container.
// Tries docker first, then podman (avoids wrong default when DOCKER_HOST is unset), with a bounded wait.
func publishedTCPPort(containerName, containerTCPPort string) (host string, port uint, found bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, cli := range []string{"docker", "podman"} {
		//nolint:gosec // G204: cli is docker|podman; name/port are fixed integration constants.
		cmd := exec.CommandContext(ctx, cli, "port", containerName, containerTCPPort)
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		h, p, ok := parseHostPort(string(out))
		if ok {
			return h, p, true
		}
	}
	return "", 0, false
}

// IntegrationStackTCPReachable is true when host-published ports for the integration
// Postgres, Redis, and Alertmanager containers accept a TCP connection.
func IntegrationStackTCPReachable() bool {
	probes := []struct {
		name string
		spec string
	}{
		{IntegrationPostgresContainer, "5432/tcp"},
		{IntegrationRedisContainer, "6379/tcp"},
		{IntegrationAlertmanagerContainer, "9093/tcp"},
	}
	for _, p := range probes {
		h, prt, ok := publishedTCPPort(p.name, p.spec)
		if !ok {
			return false
		}
		addr := net.JoinHostPort(h, strconv.FormatUint(uint64(prt), 10))
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			return false
		}
		_ = c.Close()
	}
	return true
}

func parseHostPort(output string) (host string, port uint, ok bool) {
	line := strings.TrimSpace(output)
	if line == "" {
		return "", 0, false
	}
	if idx := strings.IndexByte(line, '\n'); idx >= 0 {
		line = strings.TrimSpace(line[:idx])
	}
	lastColon := strings.LastIndex(line, ":")
	if lastColon <= 0 || lastColon >= len(line)-1 {
		return "", 0, false
	}
	hostRaw := strings.TrimSpace(line[:lastColon])
	portStr := strings.TrimSpace(line[lastColon+1:])
	p64, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		return "", 0, false
	}
	hostRaw = strings.Trim(hostRaw, "[]")
	switch hostRaw {
	case "0.0.0.0", "::":
		host = "127.0.0.1"
	default:
		host = hostRaw
	}
	return host, uint(p64), true
}

func integrationPostgresPublished() (host string, port uint, ok bool) {
	return publishedTCPPort(IntegrationPostgresContainer, "5432/tcp")
}

// ApplyIntegrationConnectionOverrides points DB (and KV / Alertmanager when present in cfg) at
// published ports for the integration stack when flightctl-integration-postgres is running.
// If that container is absent, cfg is unchanged (e.g. unit tests using localhost defaults).
// If Postgres is up, Redis and Alertmanager must be published too or the process exits.
func ApplyIntegrationConnectionOverrides(cfg *config.Config) {
	h, p, ok := integrationPostgresPublished()
	if !ok {
		return
	}
	cfg.Database.Hostname = h
	cfg.Database.Port = p

	if cfg.Alertmanager != nil {
		ah, ap, ok := publishedTCPPort(IntegrationAlertmanagerContainer, "9093/tcp")
		if !ok {
			logrus.Fatalf("integration Alertmanager container %q is not running or has no published port 9093/tcp (start with: make start-integration-services)", IntegrationAlertmanagerContainer)
		}
		cfg.Alertmanager.Hostname = ah
		cfg.Alertmanager.Port = ap
	}
	if cfg.KV != nil {
		kh, kp, ok := publishedTCPPort(IntegrationRedisContainer, "6379/tcp")
		if !ok {
			logrus.Fatalf("integration Redis container %q is not running or has no published port 6379/tcp (start with: make start-integration-services)", IntegrationRedisContainer)
		}
		cfg.KV.Hostname = kh
		cfg.KV.Port = kp
	}
}

// IntegrationRedisHost returns the Redis host for tests. When integration Postgres is running,
// Redis must be the integration Redis container; otherwise localhost (unit tests / no stack).
func IntegrationRedisHost() string {
	_, _, pgUp := integrationPostgresPublished()
	if !pgUp {
		return "localhost"
	}
	h, _, ok := publishedTCPPort(IntegrationRedisContainer, "6379/tcp")
	if !ok {
		logrus.Fatalf("integration Redis container %q is not running or has no published port 6379/tcp (start with: make start-integration-services)", IntegrationRedisContainer)
	}
	return h
}

// IntegrationRedisPort returns the Redis port for tests (integration stack or default 6379).
func IntegrationRedisPort() uint {
	_, _, pgUp := integrationPostgresPublished()
	if !pgUp {
		return 6379
	}
	_, p, ok := publishedTCPPort(IntegrationRedisContainer, "6379/tcp")
	if !ok {
		logrus.Fatalf("integration Redis container %q is not running or has no published port 6379/tcp (start with: make start-integration-services)", IntegrationRedisContainer)
	}
	return p
}

// IntegrationRedisPassword returns the Redis password for integration tests.
// Reads KV_PASSWORD, then FLIGHTCTL_KV_PASSWORD (same as make integration-test), else adminpass
// to match test/integration/integrationstack Redis --requirepass.
func IntegrationRedisPassword() domain.SecureString {
	if p := strings.TrimSpace(os.Getenv("KV_PASSWORD")); p != "" {
		return domain.SecureString(p)
	}
	if p := strings.TrimSpace(os.Getenv("FLIGHTCTL_KV_PASSWORD")); p != "" {
		return domain.SecureString(p)
	}
	return domain.SecureString("adminpass")
}
