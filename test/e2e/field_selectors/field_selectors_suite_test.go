package field_selectors

import (
	"context"
	"testing"

	"github.com/flightctl/flightctl/test/harness/e2e"
	testutil "github.com/flightctl/flightctl/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	suiteCtx context.Context
	harness  *e2e.Harness
)

func TestFieldSelector(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Field selectors Extension E2E Suite")
}

var _ = BeforeSuite(func() {
	suiteCtx = testutil.InitSuiteTracerForGinkgo("Field Selectors Extension E2E Suite")

	// Create harness using VM pool function but without getting a VM
	var err error
	harness, err = e2e.NewTestHarnessWithVMPool(suiteCtx, 0)
	Expect(err).ToNot(HaveOccurred())

	// Remove the VM since this test doesn't need it
	harness.VM = nil
})

var _ = AfterSuite(func() {
	if harness != nil {
		harness.Cleanup(false)
		err := harness.CleanUpAllResources()
		Expect(err).ToNot(HaveOccurred())
	}
})

const (
	templateImage    = "quay.io/redhat/rhde:9.2"
	repositoryUrl    = "https://github.com/flightctl/flightctl.git"
	devicePrefix     = "device-"
	fleetPrefix      = "fleet-"
	repositoryPrefix = "repository-"
	fleetName        = "fleet-1"
	resourceCount    = 10
)
