package crypto

import (
	"crypto/tls"
	"crypto/x509"

	fccrypto "github.com/flightctl/flightctl/pkg/crypto"
	oscrypto "github.com/openshift/library-go/pkg/crypto"
)

func TLSConfigForServer(caBundlex509 []*x509.Certificate, serverConfig *TLSCertificateConfig) (*tls.Config, *tls.Config, error) {
	return TLSConfigForServerWithClientCAs(caBundlex509, nil, serverConfig)
}

func TLSConfigForServerWithClientCAs(caBundlex509 []*x509.Certificate, clientCAs []*x509.Certificate, serverConfig *TLSCertificateConfig) (*tls.Config, *tls.Config, error) {

	certs := append(serverConfig.Certs, caBundlex509...)

	certBytes, err := oscrypto.EncodeCertificates(certs...)
	if err != nil {
		return nil, nil, err
	}
	keyBytes, err := fccrypto.PEMEncodeKey(serverConfig.Key)
	if err != nil {
		return nil, nil, err
	}
	cert, err := tls.X509KeyPair(certBytes, keyBytes)
	if err != nil {
		return nil, nil, err
	}

	// Use clientCAs if provided, otherwise fall back to caBundlex509
	clientCAPool := x509.NewCertPool()
	if clientCAs != nil {
		for _, caCert := range clientCAs {
			clientCAPool.AddCert(caCert)
		}
	} else {
		for _, caCert := range caBundlex509 {
			clientCAPool.AddCert(caCert)
		}
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS13,
	}

	agentTlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ClientCAs:    clientCAPool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}

	return tlsConfig, agentTlsConfig, nil
}

func TLSConfigForClient(caBundleX509 []*x509.Certificate, clientConfig *TLSCertificateConfig) (*tls.Config, error) {
	return TLSConfigForClientWithServerCAs(caBundleX509, clientConfig)
}

func TLSConfigForClientWithServerCAs(serverCAs []*x509.Certificate, clientConfig *TLSCertificateConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS13,
	}

	// Only set RootCAs if serverCAs is provided and not empty
	// If serverCAs is nil or empty, RootCAs will be nil and use system root CA pool
	if serverCAs != nil && len(serverCAs) > 0 {
		caPool := x509.NewCertPool()
		for _, caCert := range serverCAs {
			caPool.AddCert(caCert)
		}
		tlsConfig.RootCAs = caPool
	}

	if clientConfig != nil {
		certBytes, err := oscrypto.EncodeCertificates(clientConfig.Certs...)
		if err != nil {
			return nil, err
		}
		keyBytes, err := fccrypto.PEMEncodeKey(clientConfig.Key)
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(certBytes, keyBytes)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return tlsConfig, nil
}
