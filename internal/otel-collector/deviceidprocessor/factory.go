package deviceidprocessor

import (
	"context"
	"fmt"

	apiclient "github.com/flightctl/flightctl/internal/api/client"
	"github.com/flightctl/flightctl/internal/client"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/kvstore"
	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/processor"
	"go.opentelemetry.io/collector/processor/processorhelper"
)

// NewFactory returns a new processor factory.
func NewFactory(cfg *config.Config) processor.Factory {
	return processor.NewFactory(
		component.MustNewType("deviceid"),
		func() component.Config {
			return cfg
		},
		processor.WithMetrics(createMetricsProcessor, component.StabilityLevelAlpha),
	)
}

func createMetricsProcessor(
	ctx context.Context,
	set processor.Settings,
	cfg component.Config,
	next consumer.Metrics,
) (processor.Metrics, error) {
	config := cfg.(*config.Config)

	// Initialize KV store for caching using logrus
	logger := logrus.New()
	kvStore, err := kvstore.NewKVStore(ctx, logger, config.KV.Hostname, config.KV.Port, config.KV.Password)
	if err != nil {
		return nil, fmt.Errorf("failed to create KV store: %w", err)
	}

	// Initialize flightctl client using main config information
	flightctlClient, err := createFlightctlClient(config)
	if err != nil {
		kvStore.Close()
		return nil, fmt.Errorf("failed to create flightctl client: %w", err)
	}

	p := &deviceIdProcessor{
		flightctlClient: flightctlClient,
		kvStore:         kvStore,
	}

	return processorhelper.NewMetrics(
		ctx,
		set,
		cfg,
		next,
		p.processMetrics,
		processorhelper.WithCapabilities(consumer.Capabilities{MutatesData: true}),
	)
}

func createFlightctlClient(config *config.Config) (*apiclient.ClientWithResponses, error) {
	// Create client config using the main config information
	clientConfig := client.NewDefault()

	if config.Service.BaseUrl != "" {
		clientConfig.Service.Server = config.Service.BaseUrl
	}

	// Use the certificate authority from main config
	if config.Service.CertStore != "" {
		clientConfig.Service.CertificateAuthority = config.Service.CertStore
	}

	// Note: ServerCertName is not available in client.Service, so we'll skip it
	// The client will use the default certificate handling

	// Create the client
	flightctlClient, err := client.NewFromConfig(clientConfig, "")
	if err != nil {
		return nil, fmt.Errorf("failed to create flightctl client: %w", err)
	}

	return flightctlClient, nil
}
