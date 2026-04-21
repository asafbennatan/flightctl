package containers

import (
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/sirupsen/logrus"
)

// E2EAuxHostEnv is the env var to override the host used for registry/git/prometheus
// (e.g. when the test VM has multiple NICs and the cluster is on a different interface).
const E2EAuxHostEnv = "E2E_AUX_HOST"

// GetDockerNetwork returns the network name for testcontainers (kind, host, podman, bridge).
func GetDockerNetwork() string {
	if isKindCluster() {
		return "kind"
	}
	if os.Getenv("FLIGHTCTL_QUADLETS") != "" {
		return "host"
	}
	if IsPodman() {
		return "podman"
	}
	return "bridge"
}

// IsPodman reports whether the project expects Podman as the container runtime for tests.
func IsPodman() bool { return true }

func isKindCluster() bool {
	cmd := exec.Command("kind", "get", "clusters")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) != ""
}

// GetHostIP returns the host's external IP for container access.
func GetHostIP() string {
	if override := os.Getenv(E2EAuxHostEnv); override != "" {
		return override
	}
	conn, err := net.Dial("udp", "1.1.1.1:80")
	if err != nil {
		return "localhost"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// GetContainerHostname returns the hostname for host access from inside containers.
func GetContainerHostname() string {
	if isKindCluster() {
		return GetHostIP()
	}
	if IsPodman() {
		return "host.containers.internal"
	}
	return GetHostIP()
}

// ContainerExistsByName returns true if a container with the given name exists (running or stopped).
func ContainerExistsByName(name string) bool {
	cli := RuntimeCLIName()
	filter := NamePSFilter(cli, name)
	cmd := exec.Command(cli, "ps", "-a", "--filter", filter, "-q")
	out, err := cmd.CombinedOutput()
	if err != nil {
		logrus.Debugf("containerExistsByName %s: %v %s", name, err, string(out))
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}

// RemoveContainerByName force-removes a container by name (best effort).
func RemoveContainerByName(name string) error {
	cli := RuntimeCLIName()
	cmd := exec.Command(cli, "rm", "-f", "-v", name)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
