package config

import (
	"time"

	"github.com/flightctl/flightctl/internal/util"
)

// ImageBuilderRateLimitConfig holds rate limiting configuration for ImageBuilder API.
type ImageBuilderRateLimitConfig struct {
	Enabled        bool     `json:"enabled,omitempty"`
	Requests       int      `json:"requests,omitempty"`
	Window         int      `json:"window,omitempty"`
	TrustedProxies []string `json:"trustedProxies,omitempty"`
}

// ImageBuilderHealthChecks holds health check endpoint configuration for ImageBuilder API.
type ImageBuilderHealthChecks struct {
	Enabled          bool          `json:"enabled,omitempty"`
	ReadinessPath    string        `json:"readinessPath,omitempty"`
	LivenessPath     string        `json:"livenessPath,omitempty"`
	ReadinessTimeout util.Duration `json:"readinessTimeout,omitempty"`
}

// ImageBuilderServiceConfig holds configuration for the ImageBuilder API service.
type ImageBuilderServiceConfig struct {
	Address               string                       `json:"address,omitempty"`
	LogLevel              string                       `json:"logLevel,omitempty"`
	TLSCertFile           string                       `json:"tlsCertFile,omitempty"`
	TLSKeyFile            string                       `json:"tlsKeyFile,omitempty"`
	InsecureSkipTlsVerify bool                         `json:"insecureSkipTlsVerify,omitempty"`
	HttpReadTimeout       util.Duration                `json:"httpReadTimeout,omitempty"`
	HttpReadHeaderTimeout util.Duration                `json:"httpReadHeaderTimeout,omitempty"`
	HttpWriteTimeout      util.Duration                `json:"httpWriteTimeout,omitempty"`
	HttpIdleTimeout       util.Duration                `json:"httpIdleTimeout,omitempty"`
	HttpMaxNumHeaders     int                          `json:"httpMaxNumHeaders,omitempty"`
	HttpMaxUrlLength      int                          `json:"httpMaxUrlLength,omitempty"`
	HttpMaxRequestSize    int                          `json:"httpMaxRequestSize,omitempty"`
	RateLimit             *ImageBuilderRateLimitConfig `json:"rateLimit,omitempty"`
	HealthChecks          *ImageBuilderHealthChecks    `json:"healthChecks,omitempty"`
}

// NewDefaultImageBuilderServiceConfig returns the default configuration for the ImageBuilder API service.
func NewDefaultImageBuilderServiceConfig() *ImageBuilderServiceConfig {
	return &ImageBuilderServiceConfig{
		Address:               ":8443",
		LogLevel:              "info",
		HttpReadTimeout:       util.Duration(5 * time.Minute),
		HttpReadHeaderTimeout: util.Duration(5 * time.Minute),
		HttpWriteTimeout:      util.Duration(5 * time.Minute),
		HttpIdleTimeout:       util.Duration(5 * time.Minute),
		HttpMaxNumHeaders:     100,
		HttpMaxUrlLength:      2000,
		HttpMaxRequestSize:    50 * 1024 * 1024, // 50MB
		HealthChecks: &ImageBuilderHealthChecks{
			Enabled:          true,
			ReadinessPath:    "/readyz",
			LivenessPath:     "/healthz",
			ReadinessTimeout: util.Duration(2 * time.Second),
		},
	}
}
