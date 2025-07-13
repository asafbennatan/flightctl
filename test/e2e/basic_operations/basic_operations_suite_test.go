package basic_operations

import (
	"github.com/flightctl/flightctl/test/e2e/global_setup"
	testutil "github.com/flightctl/flightctl/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var (
	workerID int
)

// Initialize suite-specific settings
func init() {
	// VM pool cleanup is now handled in global teardown
}

// AfterSuite is no longer needed as cleanup is handled by SynchronizedAfterSuite

var _ = BeforeEach(func() {
	// Get the harness and context that were set up by the centralized SynchronizedBeforeSuite
	workerID = GinkgoParallelProcess()
	harness = global_setup.GetWorkerHarness()
	suiteCtx = global_setup.GetWorkerContext()

	GinkgoWriter.Printf("🔄 [BeforeEach] Worker %d: Setting up test context\n", workerID)

	// Create test-specific context for proper tracing
	ctx := testutil.StartSpecTracerForGinkgo(suiteCtx)

	// Set the test context in the harness
	harness.SetTestContext(ctx)

	GinkgoWriter.Printf("✅ [BeforeEach] Worker %d: Test setup completed\n", workerID)
})

var _ = AfterEach(func() {
	GinkgoWriter.Printf("🔄 [AfterEach] Worker %d: Cleaning up test resources\n", workerID)

	// Clean up test resources BEFORE switching back to suite context
	// This ensures we use the correct test ID for resource cleanup
	err := harness.CleanUpTestResources()
	Expect(err).ToNot(HaveOccurred())

	// Now restore suite context for any remaining cleanup operations
	harness.SetTestContext(suiteCtx)

	GinkgoWriter.Printf("✅ [AfterEach] Worker %d: Test cleanup completed\n", workerID)
})
