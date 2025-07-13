package agent_test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/flightctl/flightctl/test/harness/e2e"
	testutil "github.com/flightctl/flightctl/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const TIMEOUT = "5m"
const POLLING = "125ms"
const LONGTIMEOUT = "10m"

// Define a type for messages.
type Message string

const (
	UpdateRenderedVersionSuccess    Message = "Updated to desired renderedVersion: 2"
	ComposeFile                     string  = "podman-compose.yaml"
	ExpectedNumSleepAppV1Containers string  = "3"
	ExpectedNumSleepAppV2Containers string  = "1"
	ZeroContainers                  string  = "0"
)

// String returns the string representation of a message.
func (m Message) String() string {
	return string(m)
}

var (
	suiteCtx context.Context
	workerID int
	harness  *e2e.Harness
)

func TestAgent(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Agent E2E Suite")
}

var _ = BeforeSuite(func() {
	e2e.RegisterVMPoolCleanup()
	suiteCtx = testutil.InitSuiteTracerForGinkgo("Agent E2E Suite")
	workerID = GinkgoParallelProcess()

	fmt.Printf("🔄 [BeforeSuite] Worker %d: Starting VM and harness setup\n", workerID)

	// Setup VM for this worker using the global pool
	var err error
	_, err = e2e.SetupVMForWorker(workerID, os.TempDir(), 2233)
	Expect(err).ToNot(HaveOccurred())

	// Create harness once for the entire suite
	harness, err = e2e.NewTestHarnessWithVMPool(suiteCtx, workerID)
	Expect(err).ToNot(HaveOccurred())

	fmt.Printf("✅ [BeforeSuite] Worker %d: VM and harness setup completed successfully\n", workerID)
})

var _ = AfterSuite(func() {
	fmt.Printf("🔄 [AfterSuite] Worker %d: Starting cleanup\n", workerID)

	// Clean up harness
	if harness != nil {
		harness.Cleanup(true)
	}

	// Clean up this worker's VM
	err := e2e.CleanupVMForWorker(workerID)
	Expect(err).ToNot(HaveOccurred())

	fmt.Printf("✅ [AfterSuite] Worker %d: Cleanup completed successfully\n", workerID)
})

var _ = BeforeEach(func() {
	fmt.Printf("🔄 [BeforeEach] Worker %d: Setting up test with VM from pool\n", workerID)

	// Create test-specific context for proper tracing
	ctx := testutil.StartSpecTracerForGinkgo(suiteCtx)

	// Set the test context in the harness
	harness.SetTestContext(ctx)

	// Setup VM from pool, revert to pristine snapshot, and start agent
	err := harness.SetupVMFromPoolAndStartAgent(workerID)
	Expect(err).ToNot(HaveOccurred())

	fmt.Printf("✅ [BeforeEach] Worker %d: Test setup completed\n", workerID)
})

var _ = AfterEach(func() {
	fmt.Printf("🔄 [AfterEach] Worker %d: Cleaning up test resources\n", workerID)

	// Restore suite context for cleanup operations
	harness.SetTestContext(suiteCtx)

	err := harness.CleanUpAllResources()
	Expect(err).ToNot(HaveOccurred())

	fmt.Printf("✅ [AfterEach] Worker %d: Test cleanup completed\n", workerID)
})
