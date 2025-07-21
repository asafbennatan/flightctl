package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"google.golang.org/grpc/credentials"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Collector struct {
		Endpoint string `yaml:"endpoint"`
		Mode     string `yaml:"mode"` // "insecure", "tls", or "mtls"
		TLS      struct {
			CAFile string `yaml:"ca_file"`
		} `yaml:"tls"`
		MTLS struct {
			CertFile string `yaml:"cert_file"`
			KeyFile  string `yaml:"key_file"`
			CAFile   string `yaml:"ca_file"`
		} `yaml:"mtls"`
	} `yaml:"collector"`
	Service struct {
		Name    string `yaml:"name"`
		Version string `yaml:"version"`
	} `yaml:"service"`
	Telemetry struct {
		Interval time.Duration `yaml:"interval"`
	} `yaml:"telemetry"`
}

func loadConfig(configPath string) (*Config, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	return &config, nil
}

func createTLSConfig(config *Config) (*tls.Config, error) {
	switch config.Collector.Mode {
	case "insecure":
		return nil, nil
	case "tls":
		// TLS mode - only verify server certificate
		var caCertPool *x509.CertPool
		if config.Collector.TLS.CAFile != "" {
			caCert, err := os.ReadFile(config.Collector.TLS.CAFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool = x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to append CA certificate")
			}
		}
		return &tls.Config{
			RootCAs: caCertPool,
		}, nil
	case "mtls":
		// mTLS mode - client certificate + server verification
		cert, err := tls.LoadX509KeyPair(config.Collector.MTLS.CertFile, config.Collector.MTLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}

		var caCertPool *x509.CertPool
		if config.Collector.MTLS.CAFile != "" {
			caCert, err := os.ReadFile(config.Collector.MTLS.CAFile)
			if err != nil {
				return nil, fmt.Errorf("failed to read CA certificate: %w", err)
			}
			caCertPool = x509.NewCertPool()
			if !caCertPool.AppendCertsFromPEM(caCert) {
				return nil, fmt.Errorf("failed to append CA certificate")
			}
		}

		return &tls.Config{
			Certificates: []tls.Certificate{cert},
			RootCAs:      caCertPool,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported connection mode: %s", config.Collector.Mode)
	}
}

func createMeterProvider(config *Config) (*sdkmetric.MeterProvider, error) {
	// Create resource
	res, err := resource.New(context.Background(),
		resource.WithAttributes(
			semconv.ServiceName(config.Service.Name),
			semconv.ServiceVersion(config.Service.Version),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP exporter options
	opts := []otlpmetricgrpc.Option{
		otlpmetricgrpc.WithEndpoint(config.Collector.Endpoint),
	}

	// Handle different connection modes
	switch config.Collector.Mode {
	case "insecure":
		opts = append(opts, otlpmetricgrpc.WithInsecure())
	case "tls", "mtls":
		tlsConfig, err := createTLSConfig(config)
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS config: %w", err)
		}
		if tlsConfig != nil {
			opts = append(opts, otlpmetricgrpc.WithTLSCredentials(credentials.NewTLS(tlsConfig)))
		}
	default:
		return nil, fmt.Errorf("unsupported connection mode: %s", config.Collector.Mode)
	}

	// Create OTLP exporter
	exporter, err := otlpmetricgrpc.New(context.Background(), opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create meter provider
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(exporter)),
		sdkmetric.WithResource(res),
	)

	return mp, nil
}

func generateFakeMetrics(ctx context.Context, meter metric.Meter, config *Config) {
	// Create various metrics
	counter, _ := meter.Int64Counter("requests_total", metric.WithDescription("Total number of requests"))
	gauge, _ := meter.Float64ObservableGauge("cpu_usage", metric.WithDescription("CPU usage percentage"))
	histogram, _ := meter.Float64Histogram("request_duration", metric.WithDescription("Request duration in seconds"))

	// Register callback for gauge
	meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		// Simulate CPU usage between 0-100%
		cpuUsage := rand.Float64() * 100
		observer.ObserveFloat64(gauge, cpuUsage)
		return nil
	}, gauge)

	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Increment request counter
			counter.Add(ctx, 1, metric.WithAttributes(
				semconv.HTTPMethod("GET"),
				semconv.HTTPStatusCode(200),
			))

			// Record request duration
			start := time.Now()
			time.Sleep(time.Duration(rand.Intn(100)) * time.Millisecond)
			duration := time.Since(start).Seconds()

			histogram.Record(ctx, duration, metric.WithAttributes(
				semconv.HTTPMethod("GET"),
				semconv.HTTPStatusCode(200),
			))

			// Wait before next telemetry
			time.Sleep(config.Telemetry.Interval)
		}
	}
}

func main() {
	configPath := "/etc/otel-sample/config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	// Load configuration
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Create meter provider
	mp, err := createMeterProvider(config)
	if err != nil {
		log.Fatalf("Failed to create meter provider: %v", err)
	}
	defer func() {
		if err := mp.Shutdown(context.Background()); err != nil {
			log.Printf("Error shutting down meter provider: %v", err)
		}
	}()

	// Set global meter provider
	otel.SetMeterProvider(mp)

	// Get meter
	meter := otel.Meter(config.Service.Name)

	log.Printf("Starting OpenTelemetry sample app")
	log.Printf("Service: %s v%s", config.Service.Name, config.Service.Version)
	log.Printf("Collector endpoint: %s", config.Collector.Endpoint)
	log.Printf("Connection mode: %s", config.Collector.Mode)
	log.Printf("Telemetry interval: %v", config.Telemetry.Interval)

	// Generate fake metrics
	ctx := context.Background()
	generateFakeMetrics(ctx, meter, config)
}
