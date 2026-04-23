package util

import (
	"net"
	"strconv"

	"github.com/flightctl/flightctl/internal/domain"
	storetestutil "github.com/flightctl/flightctl/internal/store/testutil"
)

// IntegrationRedisHost returns the Redis hostname for integration tests (discovered from
// published ports on flightctl-integration-redis when running; otherwise localhost).
func IntegrationRedisHost() string {
	return storetestutil.IntegrationRedisHost()
}

// IntegrationRedisPort returns the Redis port for integration tests.
func IntegrationRedisPort() uint {
	return storetestutil.IntegrationRedisPort()
}

// IntegrationRedisPassword returns the Redis password for integration tests.
func IntegrationRedisPassword() domain.SecureString {
	return storetestutil.IntegrationRedisPassword()
}

// IntegrationRedisAddr returns host:port for Redis.
func IntegrationRedisAddr() string {
	return net.JoinHostPort(IntegrationRedisHost(), strconv.FormatUint(uint64(IntegrationRedisPort()), 10))
}
