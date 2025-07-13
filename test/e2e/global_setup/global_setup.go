package global_setup

import (
	"context"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/flightctl/flightctl/test/harness/e2e"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
)

var (
	// Per-worker storage
	workerHarnesses sync.Map // map[int]*e2e.Harness
	workerContexts  sync.Map // map[int]context.Context

	// Track if synchronized hooks are already registered
	synchronizedHooksRegistered sync.Once
)

// RegisterSynchronizedHooks automatically registers SynchronizedBeforeSuite and SynchronizedAfterSuite hooks.
// This function is called automatically when the package is imported, but can also be called manually.
func RegisterSynchronizedHooks() {
	synchronizedHooksRegistered.Do(func() {
		// Register SynchronizedBeforeSuite
		var _ = ginkgo.SynchronizedBeforeSuite(
			// This runs only once on Node 1 (global setup)
			func() []byte {
				ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedBeforeSuite] Node 1: Global setup\n")
				return nil
			},
			// This runs once per worker process, before any suite on that worker
			func(data []byte) {
				workerID := ginkgo.GinkgoParallelProcess()
				ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedBeforeSuite] Worker %d: Setting up VM and harness\n", workerID)

				// Create suite context for tracing
				suiteCtx := context.Background() // You can replace this with your tracing setup if needed

				// Setup VM for this worker using the global pool
				_, err := e2e.SetupVMForWorker(workerID, os.TempDir(), 2233)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Create harness once for the entire worker
				harness, err := e2e.NewTestHarnessWithVMPool(suiteCtx, workerID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Store the harness and context for this worker
				workerHarnesses.Store(workerID, harness)
				workerContexts.Store(workerID, suiteCtx)

				// Run global setup (this will only run once across all test suites)
				ginkgo.GinkgoWriter.Printf("🔄 [Global E2E Setup] Running global initialization for all E2E tests...\n")
				_ = harness.CleanUpAllResources()
				ginkgo.GinkgoWriter.Printf("✅ [Global E2E Setup] Global initialization completed\n")

				ginkgo.GinkgoWriter.Printf("✅ [SynchronizedBeforeSuite] Worker %d: VM and harness setup completed\n", workerID)
			},
		)

		// Register SynchronizedAfterSuite
		var _ = ginkgo.SynchronizedAfterSuite(
			// This runs once per worker process, after all suites on that worker
			func() {
				workerID := ginkgo.GinkgoParallelProcess()
				ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedAfterSuite] Worker %d: Starting cleanup\n", workerID)

				// Clean up harness
				if h, ok := workerHarnesses.Load(workerID); ok {
					harness := h.(*e2e.Harness)
					if harness != nil {
						harness.Cleanup(true)
					}
					workerHarnesses.Delete(workerID)
				}

				// Clean up this worker's VM
				err := e2e.CleanupVMForWorker(workerID)
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				// Clean up context
				workerContexts.Delete(workerID)

				ginkgo.GinkgoWriter.Printf("✅ [SynchronizedAfterSuite] Worker %d: Cleanup completed\n", workerID)
			},
			// This runs only once on Node 1 (global cleanup)
			func() {
				ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedAfterSuite] Node 1: Global cleanup\n")
				// Run global teardown (this will only run once across all test suites)
				// Note: This runs after all workers have reported back to Node 1
				ginkgo.GinkgoWriter.Printf("🔄 [Global E2E Teardown] Running global cleanup...\n")

				// Clean up the VM pool
				err := e2e.GetVMPool().CleanupAll()
				if err != nil {
					ginkgo.GinkgoWriter.Printf("❌ [Global E2E Teardown] Global cleanup failed: %v\n", err)
				}
				ginkgo.GinkgoWriter.Printf("✅ [Global E2E Teardown] Global cleanup completed\n")
			},
		)
	})
}

// SetupWorkerHarness sets up the VM and harness for the current worker.
// This should be called from SynchronizedBeforeSuite.
func SetupWorkerHarness(suiteName string) (*e2e.Harness, context.Context) {
	workerID := ginkgo.GinkgoParallelProcess()

	ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedBeforeSuite] Worker %d: Setting up VM and harness for %s\n", workerID, suiteName)

	// Create suite context for tracing
	suiteCtx := context.Background() // You can replace this with your tracing setup if needed

	// Setup VM for this worker using the global pool
	_, err := e2e.SetupVMForWorker(workerID, os.TempDir(), 2233)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Create harness once for the entire worker
	harness, err := e2e.NewTestHarnessWithVMPool(suiteCtx, workerID)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Store the harness and context for this worker
	workerHarnesses.Store(workerID, harness)
	workerContexts.Store(workerID, suiteCtx)

	ginkgo.GinkgoWriter.Printf("✅ [SynchronizedBeforeSuite] Worker %d: VM and harness setup completed\n", workerID)

	return harness, suiteCtx
}

// GetWorkerHarness retrieves the harness for the current worker.
// This should be called from your test suite's BeforeSuite or tests.
func GetWorkerHarness() *e2e.Harness {
	workerID := ginkgo.GinkgoParallelProcess()
	h, ok := workerHarnesses.Load(workerID)
	if !ok {
		ginkgo.Fail(fmt.Sprintf("No harness found for worker %d. Make sure SetupWorkerHarness was called in SynchronizedBeforeSuite", workerID))
	}
	return h.(*e2e.Harness)
}

// GetWorkerContext retrieves the context for the current worker.
func GetWorkerContext() context.Context {
	workerID := ginkgo.GinkgoParallelProcess()
	ctx, ok := workerContexts.Load(workerID)
	if !ok {
		ginkgo.Fail(fmt.Sprintf("No context found for worker %d. Make sure SetupWorkerHarness was called in SynchronizedBeforeSuite", workerID))
	}
	return ctx.(context.Context)
}

// CleanupWorkerHarness cleans up the VM and harness for the current worker.
// This should be called from SynchronizedAfterSuite.
func CleanupWorkerHarness() {
	workerID := ginkgo.GinkgoParallelProcess()

	ginkgo.GinkgoWriter.Printf("🔄 [SynchronizedAfterSuite] Worker %d: Starting cleanup\n", workerID)

	// Clean up harness
	if h, ok := workerHarnesses.Load(workerID); ok {
		harness := h.(*e2e.Harness)
		if harness != nil {
			harness.Cleanup(true)
		}
		workerHarnesses.Delete(workerID)
	}

	// Clean up this worker's VM
	err := e2e.CleanupVMForWorker(workerID)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// Clean up context
	workerContexts.Delete(workerID)

	ginkgo.GinkgoWriter.Printf("✅ [SynchronizedAfterSuite] Worker %d: Cleanup completed\n", workerID)
}

// Note: Global setup and teardown logic is now directly in SynchronizedBeforeSuite and SynchronizedAfterSuite

// TestMain runs once when this package is imported
func TestMain(m *testing.M) {
	// This won't actually run since this package doesn't have tests,
	// but it's here for completeness
	os.Exit(m.Run())
}

// init automatically registers the synchronized hooks when the package is imported
func init() {
	RegisterSynchronizedHooks()
}
