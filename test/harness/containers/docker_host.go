// Package containers provides shared testcontainers runtime setup (Podman/Docker socket,
// Ryuk, API version) for integration preflight and e2e auxiliary services.
package containers

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
)

func init() {
	ConfigureDockerHost()
}

// ConfigureDockerHost sets up the container runtime environment for testcontainers.
func ConfigureDockerHost() {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		socketPath := detectContainerSocket()
		if socketPath == "" {
			logrus.Warn("[testcontainers] Could not detect container socket")
			return
		}
		dockerHost = fmt.Sprintf("unix://%s", socketPath)
		_ = os.Setenv("DOCKER_HOST", dockerHost)
	}
	configureProviderSettings(dockerHost)
	logContainerRuntime(dockerHost)
}

// RuntimeCLIName returns the CLI binary that matches DOCKER_HOST.
func RuntimeCLIName() string {
	dh := os.Getenv("DOCKER_HOST")
	if strings.Contains(dh, "podman") {
		return "podman"
	}
	return "docker"
}

// NamePSFilter returns a docker|podman ps --filter value for an exact container name.
// Both use an anchored pattern and regexp.QuoteMeta so metacharacters in name match literally.
func NamePSFilter(runtimeCLI, name string) string {
	_ = runtimeCLI
	return "name=^" + regexp.QuoteMeta(name) + "$"
}

func logContainerRuntime(dockerHost string) {
	runtime := "docker"
	if strings.Contains(dockerHost, "podman") {
		runtime = "podman"
	}
	ryuk := "enabled"
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "true" {
		ryuk = "disabled"
	}
	logrus.Infof("[testcontainers] Container runtime: %s (DOCKER_HOST=%s), Ryuk %s", runtime, dockerHost, ryuk)
}

func configureProviderSettings(dockerHost string) {
	if os.Getenv("TESTCONTAINERS_RYUK_DISABLED") == "" {
		os.Setenv("TESTCONTAINERS_RYUK_DISABLED", "true")
	}
	if strings.Contains(dockerHost, "podman") && os.Getenv("DOCKER_API_VERSION") == "" {
		os.Setenv("DOCKER_API_VERSION", "1.43")
	}
}

func detectContainerSocket() string {
	uid := os.Getuid()
	currentUser, _ := user.Current()
	var homeDir string
	if currentUser != nil {
		homeDir = currentUser.HomeDir
	}
	var paths []string
	if xdg := os.Getenv("XDG_RUNTIME_DIR"); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "podman", "podman.sock"))
	}
	if uid != 0 {
		paths = append(paths, fmt.Sprintf("/run/user/%d/podman/podman.sock", uid))
	}
	if homeDir != "" {
		paths = append(paths,
			filepath.Join(homeDir, ".local", "share", "containers", "podman", "machine", "podman.sock"),
		)
	}
	// System Podman socket (often root-only API) after Docker: unprivileged CI users (e.g. GHA
	// "runner") must not pick /run/podman/podman.sock just because it exists — prefer Docker over
	// an unusable root socket. Rootless paths above should win for Podman when primed (podman info).
	paths = append(paths, "/var/run/docker.sock", "/run/podman/podman.sock")
	for _, p := range paths {
		if isSocket(p) {
			return p
		}
	}
	return ""
}

func isSocket(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.Mode()&os.ModeSocket != 0
}
