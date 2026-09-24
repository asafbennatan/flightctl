package quadlet

import (
	"fmt"
	"path/filepath"

	"github.com/flightctl/flightctl/test/e2e/infra"
	"github.com/sirupsen/logrus"
)

var workerRegistriesDirs = []string{
	"/etc/flightctl/flightctl-delta-worker/registries.conf.d",
	"/etc/flightctl/flightctl-worker/registries.conf.d",
}

var workerRegistryCertsDirs = []string{
	"/etc/flightctl/flightctl-delta-worker/certs.d",
	"/etc/flightctl/flightctl-worker/certs.d",
}

func (p *InfraProvider) ApplyDeltaWorkerRegistryRemap(registryURL string) error {
	remap, insecure := infra.DeltaWorkerRegistryRemapFiles(registryURL)
	caCert, err := infra.DeltaWorkerRegistryCACert()
	if err != nil {
		return err
	}
	certDir, err := infra.DeltaWorkerRegistryCertDir(registryURL)
	if err != nil {
		return err
	}
	for _, dir := range workerRegistriesDirs {
		if err := p.writeRegistriesDir(dir, remap, insecure); err != nil {
			return err
		}
	}
	for _, dir := range workerRegistryCertsDirs {
		registryCertDir := filepath.Join(dir, certDir)
		if _, err := p.RunCommand("mkdir", "-p", registryCertDir); err != nil {
			return fmt.Errorf("mkdir %s: %w", registryCertDir, err)
		}
		if err := p.WriteHostFile(filepath.Join(registryCertDir, "ca.crt"), caCert); err != nil {
			return err
		}
	}
	logrus.Infof("Quadlet: wrote worker registry remap for %s", registryURL)
	return nil
}

func (p *InfraProvider) writeRegistriesDir(dir, remap, insecure string) error {
	if _, err := p.RunCommand("mkdir", "-p", dir); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := p.WriteHostFile(filepath.Join(dir, "flightctl-remap.conf"), []byte(remap)); err != nil {
		return err
	}
	return p.WriteHostFile(filepath.Join(dir, "flightctl-e2e.conf"), []byte(insecure))
}
