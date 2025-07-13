package e2e

import (
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/flightctl/flightctl/test/harness/e2e/vm"
)

// VMPool manages VMs across all test suites
type VMPool struct {
	vms            map[int]vm.TestVMInterface
	mutex          sync.RWMutex
	config         VMPoolConfig
	storageManager *vm.StoragePoolManager
}

// VMPoolConfig holds configuration for the VM pool
type VMPoolConfig struct {
	BaseDiskPath string
	TempDir      string
	SSHPortBase  int
}

var (
	globalVMPool *VMPool
	poolOnce     sync.Once
)

// GetVMPool returns the global VM pool instance
func GetVMPool() *VMPool {
	poolOnce.Do(func() {
		globalVMPool = &VMPool{
			vms: make(map[int]vm.TestVMInterface),
		}
	})
	return globalVMPool
}

// Initialize initializes the VM pool with configuration
func (p *VMPool) Initialize(config VMPoolConfig) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()
	p.config = config

	// Initialize storage pool manager
	p.storageManager = vm.NewStoragePoolManager()

	// Ensure storage pool exists
	if err := p.storageManager.EnsureStoragePoolExists(); err != nil {
		return fmt.Errorf("failed to ensure storage pool exists: %w", err)
	}

	return nil
}

// GetVMForWorker returns a VM for the given worker ID, creating it if necessary
func (p *VMPool) GetVMForWorker(workerID int) (vm.TestVMInterface, error) {
	p.mutex.RLock()
	if vm, exists := p.vms[workerID]; exists {
		p.mutex.RUnlock()
		return vm, nil
	}
	p.mutex.RUnlock()

	// Need to create a new VM
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// Double-check after acquiring write lock
	if vm, exists := p.vms[workerID]; exists {
		return vm, nil
	}

	// Create new VM for this worker
	newVM, err := p.createVMForWorker(workerID)
	if err != nil {
		return nil, fmt.Errorf("failed to create VM for worker %d: %w", workerID, err)
	}

	p.vms[workerID] = newVM
	return newVM, nil
}

// createVMForWorker creates a new VM for the specified worker
func (p *VMPool) createVMForWorker(workerID int) (vm.TestVMInterface, error) {
	vmName := fmt.Sprintf("flightctl-e2e-worker-%d", workerID)

	fmt.Printf("🔄 [VMPool] Worker %d: Creating VM %s\n", workerID, vmName)

	// Create worker-specific temp directory
	workerDir := filepath.Join(p.config.TempDir, fmt.Sprintf("flightctl-e2e-worker-%d", workerID))
	if err := os.MkdirAll(workerDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create worker directory: %w", err)
	}

	// Create QCOW2 overlay in storage pool
	overlayDisk, err := p.storageManager.CreateOverlayInPool(workerID, p.config.BaseDiskPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create overlay disk in storage pool: %w", err)
	}
	fmt.Printf("✅ [VMPool] Worker %d: Overlay disk created successfully in storage pool\n", workerID)

	// Create VM using the overlay disk
	newVM, err := vm.NewVM(vm.TestVM{
		TestDir:       workerDir,
		VMName:        vmName,
		DiskImagePath: overlayDisk, // Use overlay, not base
		VMUser:        "user",
		SSHPassword:   "user",
		SSHPort:       p.config.SSHPortBase + workerID,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create VM: %w", err)
	}
	fmt.Printf("✅ [VMPool] Worker %d: VM struct created\n", workerID)

	// Clean up any existing VM with the same name
	fmt.Printf("🔄 [VMPool] Worker %d: Checking for existing VM\n", workerID)
	if err := p.cleanupExistingVM(newVM); err != nil {
		return nil, fmt.Errorf("failed to cleanup existing VM: %w", err)
	}
	fmt.Printf("✅ [VMPool] Worker %d: Existing VM cleanup completed\n", workerID)

	// Start the VM and wait for SSH to be ready
	fmt.Printf("🔄 [VMPool] Worker %d: Starting VM and waiting for SSH\n", workerID)
	if err := newVM.RunAndWaitForSSH(); err != nil {
		return nil, fmt.Errorf("failed to start VM: %w", err)
	}
	fmt.Printf("✅ [VMPool] Worker %d: VM started and SSH ready\n", workerID)

	// Take a snapshot of the running state (VM stays running)
	fmt.Printf("🔄 [VMPool] Worker %d: Creating pristine snapshot\n", workerID)
	if err := newVM.CreateSnapshot("pristine"); err != nil {
		// Clean up on failure
		_ = newVM.ForceDelete()
		return nil, fmt.Errorf("failed to create pristine snapshot: %w", err)
	}
	fmt.Printf("✅ [VMPool] Worker %d: Pristine snapshot created successfully\n", workerID)

	// VM stays running - no shutdown
	fmt.Printf("✅ [VMPool] Worker %d: VM setup completed, VM is running\n", workerID)
	return newVM, nil
}

// cleanupExistingVM removes any existing VM with the same name
func (p *VMPool) cleanupExistingVM(newVM vm.TestVMInterface) error {
	// Check if VM exists
	exists, err := newVM.Exists()
	if err != nil {
		return fmt.Errorf("failed to check if VM exists: %w", err)
	}

	if exists {
		fmt.Printf("🔄 [VMPool] Found existing VM, deleting it\n")
		// Force delete the existing VM
		if err := newVM.ForceDelete(); err != nil {
			return fmt.Errorf("failed to delete existing VM: %w", err)
		}

		// Wait a moment for cleanup to complete
		time.Sleep(1 * time.Second)
		fmt.Printf("✅ [VMPool] Existing VM deleted successfully\n")
	} else {
		fmt.Printf("✅ [VMPool] No existing VM found\n")
	}

	return nil
}

// CleanupWorkerVM cleans up the VM for a specific worker
func (p *VMPool) CleanupWorkerVM(workerID int) error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if vm, exists := p.vms[workerID]; exists {
		fmt.Printf("🔄 [VMPool] Worker %d: Starting VM cleanup\n", workerID)

		// Check if VM still exists before trying to shut it down
		vmExists, err := vm.Exists()
		if err != nil {
			fmt.Printf("⚠️  [VMPool] Worker %d: Failed to check if VM exists: %v\n", workerID, err)
		}

		if vmExists {
			// Shutdown VM if it's running
			fmt.Printf("🔄 [VMPool] Worker %d: Shutting down VM\n", workerID)
			if err := vm.Shutdown(); err != nil {
				fmt.Printf("⚠️  [VMPool] Worker %d: Failed to shutdown VM: %v\n", workerID, err)
			}

			// Wait a moment for shutdown to complete
			time.Sleep(2 * time.Second)

			// Delete snapshot first (ignore errors as VM might be gone)
			fmt.Printf("🔄 [VMPool] Worker %d: Deleting snapshot\n", workerID)
			_ = vm.DeleteSnapshot("pristine")

			// Delete VM
			fmt.Printf("🔄 [VMPool] Worker %d: Deleting VM\n", workerID)
			if err := vm.ForceDelete(); err != nil {
				fmt.Printf("⚠️  [VMPool] Worker %d: Failed to delete VM: %v\n", workerID, err)
			}
		} else {
			fmt.Printf("ℹ️  [VMPool] Worker %d: VM no longer exists, skipping shutdown\n", workerID)
		}

		// Clean up overlay disk
		fmt.Printf("🔄 [VMPool] Worker %d: Cleaning up overlay disk\n", workerID)
		if err := p.storageManager.CleanupWorkerOverlay(workerID); err != nil {
			fmt.Printf("⚠️  [VMPool] Worker %d: Failed to cleanup overlay disk: %v\n", workerID, err)
		}

		delete(p.vms, workerID)
		fmt.Printf("✅ [VMPool] Worker %d: VM cleanup completed successfully\n", workerID)
	} else {
		fmt.Printf("✅ [VMPool] Worker %d: No VM found to cleanup\n", workerID)
	}
	return nil
}

// CleanupAll cleans up all VMs in the pool
func (p *VMPool) CleanupAll() error {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	var lastErr error
	for workerID, vm := range p.vms {
		fmt.Printf("🔄 [VMPool] Worker %d: Cleaning up VM\n", workerID)

		// Check if VM exists before trying to clean it up
		vmExists, err := vm.Exists()
		if err != nil {
			fmt.Printf("⚠️  [VMPool] Worker %d: Failed to check if VM exists: %v\n", workerID, err)
		}

		if vmExists {
			// Delete snapshot first (ignore errors)
			_ = vm.DeleteSnapshot("pristine")

			// Delete VM
			if err := vm.ForceDelete(); err != nil {
				fmt.Printf("⚠️  [VMPool] Worker %d: Failed to delete VM: %v\n", workerID, err)
				if lastErr != nil {
					lastErr = fmt.Errorf("multiple errors: %w, worker %d: %w", lastErr, workerID, err)
				} else {
					lastErr = fmt.Errorf("failed to delete VM for worker %d: %w", workerID, err)
				}
			}
		} else {
			fmt.Printf("ℹ️  [VMPool] Worker %d: VM no longer exists, skipping cleanup\n", workerID)
		}
	}
	p.vms = make(map[int]vm.TestVMInterface)

	// Clean up all overlays
	if p.storageManager != nil {
		if err := p.storageManager.CleanupAllOverlays(); err != nil {
			fmt.Printf("⚠️  [VMPool] Failed to cleanup overlays: %v\n", err)
			if lastErr != nil {
				lastErr = fmt.Errorf("multiple errors: %w, overlay cleanup: %w", lastErr, err)
			} else {
				lastErr = fmt.Errorf("failed to cleanup overlays: %w", err)
			}
		}
	}

	return lastErr
}

// GetBaseDiskPath finds the base qcow2 disk path
func GetBaseDiskPath() (string, error) {
	currentWorkDirectory, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	parts := strings.Split(currentWorkDirectory, "/")
	topDir := ""
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] == "test" {
			topDir = strings.Join(parts[:i], "/")
			break
		}
	}

	if topDir == "" {
		return "", fmt.Errorf("could not find top-level directory")
	}

	baseDisk := filepath.Join(topDir, "bin/output/qcow2/disk.qcow2")
	if _, err := os.Stat(baseDisk); os.IsNotExist(err) {
		return "", fmt.Errorf("base disk not found at %s", baseDisk)
	}

	return baseDisk, nil
}

// SetupVMForWorker is a convenience function that initializes the VM pool and returns a VM for the worker
func SetupVMForWorker(workerID int, tempDir string, sshPortBase int) (vm.TestVMInterface, error) {
	vmPool := GetVMPool()

	baseDiskPath, err := GetBaseDiskPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get base disk path: %w", err)
	}

	if err := vmPool.Initialize(VMPoolConfig{
		BaseDiskPath: baseDiskPath,
		TempDir:      tempDir,
		SSHPortBase:  sshPortBase,
	}); err != nil {
		return nil, fmt.Errorf("failed to initialize VM pool: %w", err)
	}

	return vmPool.GetVMForWorker(workerID)
}

// CleanupVMForWorker is a convenience function to clean up a worker's VM
func CleanupVMForWorker(workerID int) error {
	vmPool := GetVMPool()
	return vmPool.CleanupWorkerVM(workerID)
}

// RegisterVMPoolCleanup sets up a signal handler to clean up all VMs on process exit
var cleanupRegistered sync.Once

func RegisterVMPoolCleanup() {
	cleanupRegistered.Do(func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-c
			GetVMPool().CleanupAll()
			os.Exit(1)
		}()
	})
}
