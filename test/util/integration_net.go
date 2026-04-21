package util

import (
	"net"
	"strconv"

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

// IntegrationRedisAddr returns host:port for Redis.
func IntegrationRedisAddr() string {
	return net.JoinHostPort(IntegrationRedisHost(), strconv.FormatUint(uint64(IntegrationRedisPort()), 10))
}
