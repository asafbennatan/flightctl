//go:build integration

package testutil

import (
	"context"

	"github.com/flightctl/flightctl/test/integration/integrationstack"
	"github.com/sirupsen/logrus"
)

func ensureIntegrationStackIfNeeded(ctx context.Context) {
	if err := integrationstack.EnsureRunning(ctx); err != nil {
		logrus.Fatalf("integration stack: %v", err)
	}
}
