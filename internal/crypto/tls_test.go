package crypto

import (
	"crypto/x509"
	"testing"

	"github.com/flightctl/flightctl/internal/config/ca"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTLSConfigForServerWithClientCAs(t *testing.T) {
	// Create a test CA
	caConfig := ca.NewDefault(t.TempDir())
	
	caClient, _, err := EnsureCA(caConfig)
	require.NoError(t, err)
	
	// Create a server certificate
	serverCert, err := caClient.MakeServerCertificate(nil, []string{"localhost"}, 365)
	require.NoError(t, err)
	
	// Test with same CA bundle for server and client
	tlsConfig, agentTlsConfig, err := TLSConfigForServerWithClientCAs(
		caClient.GetCABundleX509(),
		nil, // Use same CA bundle for client validation
		serverCert,
	)
	require.NoError(t, err)
	
	// Verify both configurations are created
	assert.NotNil(t, tlsConfig)
	assert.NotNil(t, agentTlsConfig)
	
	// Verify that regular TLS config doesn't have client auth
	assert.Empty(t, tlsConfig.ClientCAs)
	assert.Equal(t, 0, int(tlsConfig.ClientAuth))
	
	// Verify that agent TLS config has client auth enabled
	assert.NotNil(t, agentTlsConfig.ClientCAs)
	assert.Equal(t, 4, int(agentTlsConfig.ClientAuth)) // RequireAndVerifyClientCert = 4
	
	// Test with separate client CA bundle
	separateClientCA := caClient.GetCABundleX509()
	_, agentTlsConfig2, err := TLSConfigForServerWithClientCAs(
		caClient.GetCABundleX509(),
		separateClientCA,
		serverCert,
	)
	require.NoError(t, err)
	
	// Verify client CA pool is set correctly
	assert.NotNil(t, agentTlsConfig2.ClientCAs)
	
	// Test backward compatibility - should work the same as original function
	tlsConfig3, agentTlsConfig3, err := TLSConfigForServer(caClient.GetCABundleX509(), serverCert)
	require.NoError(t, err)
	
	assert.NotNil(t, tlsConfig3)
	assert.NotNil(t, agentTlsConfig3)
	assert.Equal(t, int(agentTlsConfig.ClientAuth), int(agentTlsConfig3.ClientAuth))
}

func TestTLSConfigForClientWithServerCAs(t *testing.T) {
	// Create a test CA
	caConfig := ca.NewDefault(t.TempDir())
	
	caClient, _, err := EnsureCA(caConfig)
	require.NoError(t, err)
	
	// Test with server CAs
	tlsConfig, err := TLSConfigForClientWithServerCAs(
		caClient.GetCABundleX509(),
		nil,
	)
	require.NoError(t, err)
	
	// Verify RootCAs is set
	assert.NotNil(t, tlsConfig.RootCAs)
	
	// Test with empty server CAs - should use system root CA pool
	tlsConfig2, err := TLSConfigForClientWithServerCAs(
		nil,
		nil,
	)
	require.NoError(t, err)
	
	// Verify RootCAs is nil (uses system root CA pool)
	assert.Nil(t, tlsConfig2.RootCAs)
	
	// Test with empty slice - should use system root CA pool
	tlsConfig3, err := TLSConfigForClientWithServerCAs(
		[]*x509.Certificate{},
		nil,
	)
	require.NoError(t, err)
	
	// Verify RootCAs is nil (uses system root CA pool)
	assert.Nil(t, tlsConfig3.RootCAs)
	
	// Test backward compatibility
	tlsConfig4, err := TLSConfigForClient(caClient.GetCABundleX509(), nil)
	require.NoError(t, err)
	
	assert.NotNil(t, tlsConfig4)
	assert.NotNil(t, tlsConfig4.RootCAs)
}

func TestCAClientSeparation(t *testing.T) {
	// Create a test CA
	caConfig := ca.NewDefault(t.TempDir())
	
	caClient, _, err := EnsureCA(caConfig)
	require.NoError(t, err)
	
	// Test that the new methods return the same certificates as the original
	// (for backward compatibility)
	originalBundle := caClient.GetCABundleX509()
	serverBundle := caClient.GetServerCABundleX509()
	clientBundle := caClient.GetClientCABundleX509()
	
	assert.Equal(t, originalBundle, serverBundle)
	assert.Equal(t, originalBundle, clientBundle)
	
	// Verify they contain the same certificates
	assert.Equal(t, len(originalBundle), len(serverBundle))
	assert.Equal(t, len(originalBundle), len(clientBundle))
	
	if len(originalBundle) > 0 {
		assert.Equal(t, originalBundle[0].Subject, serverBundle[0].Subject)
		assert.Equal(t, originalBundle[0].Subject, clientBundle[0].Subject)
	}
}