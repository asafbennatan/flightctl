package tasks

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	v1beta1 "github.com/flightctl/flightctl/api/v1beta1"
	api "github.com/flightctl/flightctl/api/v1beta1/imagebuilder"
	"github.com/flightctl/flightctl/internal/config"
	"github.com/flightctl/flightctl/internal/crypto"
	imagebuilderstore "github.com/flightctl/flightctl/internal/imagebuilder_api/store"
	"github.com/flightctl/flightctl/internal/service"
	"github.com/flightctl/flightctl/internal/store"
	"github.com/google/uuid"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
)

const (
	// agentConfigPath is the destination path for the agent config in the image
	agentConfigPath = "/etc/flightctl/config.yaml"
)

// containerfileTemplate is embedded from the templates directory for easier editing
//
//go:embed templates/Containerfile.tmpl
var containerfileTemplate string

// ContainerfileResult contains the generated Containerfile and any associated files
type ContainerfileResult struct {
	// Containerfile is the generated Containerfile content
	Containerfile string
	// AgentConfig contains the full agent config.yaml content (for early binding)
	// This includes: client-certificate-data, client-key-data, certificate-authority-data, server URL
	AgentConfig []byte
}

// processImageBuild processes an imageBuild job by loading the ImageBuild resource
// and routing to the appropriate build handler
func (c *Consumer) processImageBuild(ctx context.Context, job Job, log logrus.FieldLogger) error {
	log = log.WithField("job", job.Name).WithField("orgId", job.OrgID)
	log.Info("Processing imageBuild job")

	// Parse org ID
	orgID, err := uuid.Parse(job.OrgID)
	if err != nil {
		return fmt.Errorf("invalid org ID %q: %w", job.OrgID, err)
	}

	// Load the ImageBuild resource from the database
	imageBuild, err := c.store.ImageBuild().Get(ctx, orgID, job.Name)
	if err != nil {
		return fmt.Errorf("failed to load ImageBuild %q: %w", job.Name, err)
	}

	log.WithField("spec", imageBuild.Spec).Debug("Loaded ImageBuild resource")

	// Initialize status if nil
	if imageBuild.Status == nil {
		imageBuild.Status = &api.ImageBuildStatus{}
	}

	// Check if already completed or failed - skip if so
	if imageBuild.Status.Conditions != nil {
		for _, cond := range *imageBuild.Status.Conditions {
			if cond.Type == api.ImageBuildConditionTypeReady {
				isCompleted := cond.Reason == string(api.ImageBuildConditionReasonCompleted) && cond.Status == v1beta1.ConditionStatusTrue
				isFailed := cond.Reason == string(api.ImageBuildConditionReasonFailed) && cond.Status == v1beta1.ConditionStatusFalse
				if isCompleted || isFailed {
					log.Infof("ImageBuild %q already in terminal state %q, skipping", job.Name, cond.Reason)
					return nil
				}
			}
		}
	}

	// Start status updater goroutine - this is the single writer for all status updates
	// It handles both LastSeen (periodic) and condition updates (on-demand)
	statusUpdater, cleanupStatusUpdater := startStatusUpdater(ctx, c.store, orgID, job.Name, c.cfg, log)
	defer cleanupStatusUpdater()

	// Update status to Building
	now := time.Now().UTC()
	buildingCondition := api.ImageBuildCondition{
		Type:               api.ImageBuildConditionTypeReady,
		Status:             v1beta1.ConditionStatusFalse,
		Reason:             string(api.ImageBuildConditionReasonBuilding),
		Message:            "Build is in progress",
		LastTransitionTime: now,
	}
	statusUpdater.updateCondition(buildingCondition)

	log.Info("Updated ImageBuild status to Building")

	// Step 1: Generate Containerfile
	log.Info("Generating Containerfile for image build")
	containerfileResult, err := c.generateContainerfile(ctx, orgID, imageBuild, log)
	if err != nil {
		failedTime := time.Now().UTC()
		failedCondition := api.ImageBuildCondition{
			Type:               api.ImageBuildConditionTypeReady,
			Status:             v1beta1.ConditionStatusFalse,
			Reason:             string(api.ImageBuildConditionReasonFailed),
			Message:            err.Error(),
			LastTransitionTime: failedTime,
		}
		statusUpdater.updateCondition(failedCondition)
		return fmt.Errorf("failed to generate Containerfile: %w", err)
	}

	log.WithField("containerfile_length", len(containerfileResult.Containerfile)).Info("Containerfile generated successfully")
	log.Debug("Generated Containerfile:\n", containerfileResult.Containerfile)

	// Step 2: Start podman worker container
	podmanWorker, err := c.startPodmanWorker(ctx, orgID, imageBuild, statusUpdater, log)
	if err != nil {
		failedTime := time.Now().UTC()
		failedCondition := api.ImageBuildCondition{
			Type:               api.ImageBuildConditionTypeReady,
			Status:             v1beta1.ConditionStatusFalse,
			Reason:             string(api.ImageBuildConditionReasonFailed),
			Message:            err.Error(),
			LastTransitionTime: failedTime,
		}
		statusUpdater.updateCondition(failedCondition)
		return fmt.Errorf("failed to start podman worker: %w", err)
	}
	defer podmanWorker.Cleanup()

	// Step 3: Build with podman container in container
	err = c.buildImageWithPodman(ctx, orgID, imageBuild, containerfileResult, podmanWorker, log)
	if err != nil {
		failedTime := time.Now().UTC()
		failedCondition := api.ImageBuildCondition{
			Type:               api.ImageBuildConditionTypeReady,
			Status:             v1beta1.ConditionStatusFalse,
			Reason:             string(api.ImageBuildConditionReasonFailed),
			Message:            err.Error(),
			LastTransitionTime: failedTime,
		}
		statusUpdater.updateCondition(failedCondition)
		return fmt.Errorf("failed to build image with podman: %w", err)
	}

	// Step 4: Push image to registry
	imageRef, err := c.pushImageWithPodman(ctx, orgID, imageBuild, podmanWorker, log)
	if err != nil {
		failedTime := time.Now().UTC()
		failedCondition := api.ImageBuildCondition{
			Type:               api.ImageBuildConditionTypeReady,
			Status:             v1beta1.ConditionStatusFalse,
			Reason:             string(api.ImageBuildConditionReasonFailed),
			Message:            err.Error(),
			LastTransitionTime: failedTime,
		}
		statusUpdater.updateCondition(failedCondition)
		return fmt.Errorf("failed to push image with podman: %w", err)
	}

	// Update ImageBuild status with the pushed image reference and mark as Completed
	statusUpdater.updateImageReference(imageRef)

	// Mark as Completed
	now = time.Now().UTC()
	completedCondition := api.ImageBuildCondition{
		Type:               api.ImageBuildConditionTypeReady,
		Status:             v1beta1.ConditionStatusTrue,
		Reason:             string(api.ImageBuildConditionReasonCompleted),
		Message:            "Build completed successfully",
		LastTransitionTime: now,
	}
	statusUpdater.updateCondition(completedCondition)

	log.Info("ImageBuild marked as Completed")
	return nil
}

// statusUpdateRequest represents a request to update the ImageBuild status
type statusUpdateRequest struct {
	Condition      *api.ImageBuildCondition
	LastSeen       *time.Time
	ImageReference *string
}

// statusUpdater manages all status updates for an ImageBuild, ensuring atomic updates
// and preventing race conditions between LastSeen and condition updates.
// It also tracks task outputs and only updates LastSeen when new data is received.
type statusUpdater struct {
	store          imagebuilderstore.Store
	orgID          uuid.UUID
	imageBuildName string
	updateChan     chan statusUpdateRequest
	outputChan     chan []byte // Central channel for all task outputs
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	log            logrus.FieldLogger
}

// startStatusUpdater starts a goroutine that is the single writer for ImageBuild status updates.
// It receives condition updates via a channel and periodically updates LastSeen.
// Returns the updater and a cleanup function.
func startStatusUpdater(
	ctx context.Context,
	store imagebuilderstore.Store,
	orgID uuid.UUID,
	imageBuildName string,
	cfg *config.Config,
	log logrus.FieldLogger,
) (*statusUpdater, func()) {
	updaterCtx, updaterCancel := context.WithCancel(ctx)

	updater := &statusUpdater{
		store:          store,
		orgID:          orgID,
		imageBuildName: imageBuildName,
		updateChan:     make(chan statusUpdateRequest), // Unbuffered channel - blocks until processed
		outputChan:     make(chan []byte, 100),         // Buffered channel for task outputs
		ctx:            updaterCtx,
		cancel:         updaterCancel,
		log:            log,
	}

	updater.wg.Add(1)
	go updater.run(cfg)

	cleanup := func() {
		updaterCancel()
		close(updater.updateChan)
		close(updater.outputChan)
		updater.wg.Wait()
	}

	return updater, cleanup
}

// run is the main loop for the status updater goroutine
func (u *statusUpdater) run(cfg *config.Config) {
	defer u.wg.Done()

	// Use LastSeenUpdateInterval from config (defaults are applied during config loading)
	if cfg == nil || cfg.ImageBuilderWorker == nil {
		u.log.Error("Config or ImageBuilderWorker config is nil, cannot update status")
		return
	}
	updateInterval := time.Duration(cfg.ImageBuilderWorker.LastSeenUpdateInterval)
	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	// Track pending updates
	var pendingCondition *api.ImageBuildCondition
	var pendingImageReference *string
	lastSeenUpdateTime := time.Now().UTC()

	// Track the last time output was received - updated when new output arrives
	var lastOutputTime *time.Time
	// Track the last LastSeen value we set in the database
	var lastSetLastSeen *time.Time

	for {
		select {
		case <-u.ctx.Done():
			// Context cancelled, perform final update if needed and exit
			u.updateStatus(pendingCondition, &lastSeenUpdateTime, pendingImageReference)
			return
		case <-ticker.C:
			// Periodic LastSeen update - only if we have new output time and haven't set it yet
			if lastOutputTime != nil {
				// Only update if this is a different time than what we last set
				if lastSetLastSeen == nil || !lastOutputTime.Equal(*lastSetLastSeen) {
					lastSeenUpdateTime = *lastOutputTime
					// Store a copy of the time we're setting
					lastSetLastSeenCopy := *lastOutputTime
					lastSetLastSeen = &lastSetLastSeenCopy
					u.updateStatus(pendingCondition, &lastSeenUpdateTime, pendingImageReference)
					pendingCondition = nil      // Clear after update
					pendingImageReference = nil // Clear after update
				}
			}
		case output := <-u.outputChan:
			// Task output received - update local variable with current time
			now := time.Now().UTC()
			lastOutputTime = &now
			// Log output for debugging (can be removed or made conditional)
			u.log.Debugf("Task output: %s", string(output))
		case req := <-u.updateChan:
			// Status update requested
			if req.Condition != nil {
				pendingCondition = req.Condition
			}
			if req.LastSeen != nil {
				lastSeenUpdateTime = *req.LastSeen
			}
			if req.ImageReference != nil {
				pendingImageReference = req.ImageReference
			}
			// Update immediately when condition or image reference changes
			if req.Condition != nil || req.ImageReference != nil {
				u.updateStatus(pendingCondition, &lastSeenUpdateTime, pendingImageReference)
				pendingCondition = nil      // Clear after update
				pendingImageReference = nil // Clear after update
			}
		}
	}
}

// updateStatus performs the actual database update, merging conditions, LastSeen, and ImageReference
func (u *statusUpdater) updateStatus(condition *api.ImageBuildCondition, lastSeen *time.Time, imageReference *string) {
	// Load current status from database
	imageBuild, err := u.store.ImageBuild().Get(u.ctx, u.orgID, u.imageBuildName)
	if err != nil {
		u.log.WithError(err).Warn("Failed to load ImageBuild for status update")
		return
	}

	// Initialize status if needed
	if imageBuild.Status == nil {
		imageBuild.Status = &api.ImageBuildStatus{}
	}

	// Update LastSeen
	if lastSeen != nil {
		imageBuild.Status.LastSeen = lastSeen
	}

	// Update condition if provided
	if condition != nil {
		if imageBuild.Status.Conditions == nil {
			imageBuild.Status.Conditions = &[]api.ImageBuildCondition{}
		}

		// Use helper function to set condition, keeping ImageBuildCondition type
		api.SetImageBuildStatusCondition(imageBuild.Status.Conditions, *condition)
	}

	// Update ImageReference if provided
	if imageReference != nil {
		imageBuild.Status.ImageReference = imageReference
	}

	// Write updated status atomically
	_, err = u.store.ImageBuild().UpdateStatus(u.ctx, u.orgID, imageBuild)
	if err != nil {
		u.log.WithError(err).Warn("Failed to update ImageBuild status")
	}
}

// updateCondition sends a condition update request to the updater goroutine
func (u *statusUpdater) updateCondition(condition api.ImageBuildCondition) {
	select {
	case u.updateChan <- statusUpdateRequest{Condition: &condition}:
	case <-u.ctx.Done():
		// Context cancelled, ignore update
	}
}

// updateImageReference sends an image reference update request to the updater goroutine
func (u *statusUpdater) updateImageReference(imageReference string) {
	select {
	case u.updateChan <- statusUpdateRequest{ImageReference: &imageReference}:
	case <-u.ctx.Done():
		// Context cancelled, ignore update
	}
}

// reportOutput sends task output to the central output handler
// This marks that progress has been made and LastSeen should be updated
func (u *statusUpdater) reportOutput(output []byte) {
	select {
	case u.outputChan <- output:
	case <-u.ctx.Done():
		// Context cancelled, ignore output
	}
}

// containerfileData holds the data for rendering the Containerfile template
type containerfileData struct {
	RegistryURL         string
	ImageName           string
	ImageTag            string
	EarlyBinding        bool
	AgentConfig         string
	AgentConfigDestPath string
	HeredocDelimiter    string
}

// EnrollmentCredentialGenerator is an interface for generating enrollment credentials
// This allows for easier testing by mocking the service handler
type EnrollmentCredentialGenerator interface {
	GenerateEnrollmentCredential(ctx context.Context, orgId uuid.UUID, baseName string, ownerKind string, ownerName string) (*crypto.EnrollmentCredential, v1beta1.Status)
}

// GenerateContainerfile generates a Containerfile from an ImageBuild spec
// This function is exported for testing purposes
func GenerateContainerfile(
	ctx context.Context,
	mainStore store.Store,
	credentialGenerator EnrollmentCredentialGenerator,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	log logrus.FieldLogger,
) (*ContainerfileResult, error) {
	// Create a temporary consumer for testing purposes
	var serviceHandler *service.ServiceHandler
	if sh, ok := credentialGenerator.(*service.ServiceHandler); ok {
		serviceHandler = sh
	}
	c := &Consumer{
		mainStore:      mainStore,
		serviceHandler: serviceHandler,
		log:            log,
	}
	return c.generateContainerfileWithGenerator(ctx, orgID, imageBuild, credentialGenerator, log)
}

// generateContainerfile generates a Containerfile from an ImageBuild spec
func (c *Consumer) generateContainerfile(
	ctx context.Context,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	log logrus.FieldLogger,
) (*ContainerfileResult, error) {
	return c.generateContainerfileWithGenerator(ctx, orgID, imageBuild, c.serviceHandler, log)
}

// generateContainerfileWithGenerator generates a Containerfile from an ImageBuild spec
// credentialGenerator can be provided for testing with mocks
func (c *Consumer) generateContainerfileWithGenerator(
	ctx context.Context,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	credentialGenerator EnrollmentCredentialGenerator,
	log logrus.FieldLogger,
) (*ContainerfileResult, error) {
	// Check context before starting work
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if imageBuild == nil {
		return nil, fmt.Errorf("imageBuild cannot be nil")
	}

	spec := imageBuild.Spec

	// Load the source repository to get the registry URL
	registryURL, err := c.getRepositoryURL(ctx, orgID, spec.Source.Repository)
	if err != nil {
		return nil, fmt.Errorf("failed to get source repository URL: %w", err)
	}

	// Determine binding type
	bindingType, err := spec.Binding.Discriminator()
	if err != nil {
		return nil, fmt.Errorf("failed to determine binding type: %w", err)
	}

	log.WithFields(logrus.Fields{
		"registryURL": registryURL,
		"imageName":   spec.Source.ImageName,
		"imageTag":    spec.Source.ImageTag,
		"bindingType": bindingType,
	}).Debug("Generating Containerfile")

	result := &ContainerfileResult{}

	// Generate a unique heredoc delimiter to avoid conflicts with config content
	heredocDelimiter := fmt.Sprintf("FLIGHTCTL_CONFIG_%s", uuid.NewString()[:8])

	// Prepare template data
	data := containerfileData{
		RegistryURL:         registryURL,
		ImageName:           spec.Source.ImageName,
		ImageTag:            spec.Source.ImageTag,
		EarlyBinding:        bindingType == string(api.BindingTypeEarly),
		AgentConfigDestPath: agentConfigPath,
		HeredocDelimiter:    heredocDelimiter,
	}

	// Handle early binding - generate enrollment credentials
	if data.EarlyBinding {
		// Generate a unique name for this build's enrollment credentials
		imageBuildName := lo.FromPtr(imageBuild.Metadata.Name)
		credentialName := fmt.Sprintf("imagebuild-%s-%s", imageBuildName, orgID.String()[:8])

		agentConfig, err := c.generateAgentConfigWithGenerator(ctx, orgID, credentialName, imageBuildName, credentialGenerator)
		if err != nil {
			return nil, fmt.Errorf("failed to generate agent config for early binding: %w", err)
		}

		// Store agent config as string for template rendering
		data.AgentConfig = string(agentConfig)
		result.AgentConfig = agentConfig
		log.WithField("credentialName", credentialName).Debug("Generated agent config for early binding")
	}

	// Render the Containerfile template
	containerfile, err := renderContainerfileTemplate(data)
	if err != nil {
		return nil, fmt.Errorf("failed to render Containerfile template: %w", err)
	}

	result.Containerfile = containerfile
	return result, nil
}

// getRepositoryURL retrieves the registry URL from a Repository resource
func getRepositoryURL(ctx context.Context, mainStore store.Store, orgID uuid.UUID, repoName string) (string, error) {
	c := &Consumer{mainStore: mainStore}
	return c.getRepositoryURL(ctx, orgID, repoName)
}

// getRepositoryURL retrieves the registry URL from a Repository resource
func (c *Consumer) getRepositoryURL(ctx context.Context, orgID uuid.UUID, repoName string) (string, error) {
	repo, err := c.mainStore.Repository().Get(ctx, orgID, repoName)
	if err != nil {
		return "", fmt.Errorf("repository %q not found: %w", repoName, err)
	}

	// Get the repository spec type
	specType, err := repo.Spec.Discriminator()
	if err != nil {
		return "", fmt.Errorf("failed to determine repository spec type: %w", err)
	}

	// Only OCI repositories are supported for image builds
	if specType != string(v1beta1.RepoSpecTypeOci) {
		return "", fmt.Errorf("repository %q must be of type 'oci', got %q", repoName, specType)
	}

	ociSpec, err := repo.Spec.AsOciRepoSpec()
	if err != nil {
		return "", fmt.Errorf("failed to parse OCI repository spec: %w", err)
	}

	// Build the full registry URL with optional scheme
	registryURL := ociSpec.Registry
	if ociSpec.Scheme != nil {
		registryURL = fmt.Sprintf("%s://%s", *ociSpec.Scheme, ociSpec.Registry)
	}

	return registryURL, nil
}

// generateAgentConfigWithGenerator generates a complete agent config.yaml for early binding.
// credentialGenerator can be provided for testing with mocks
func (c *Consumer) generateAgentConfigWithGenerator(ctx context.Context, orgID uuid.UUID, name string, imageBuildName string, credentialGenerator EnrollmentCredentialGenerator) ([]byte, error) {
	// Generate enrollment credential using the credential generator
	// This will create a CSR, auto-approve it, sign it, and return the credential
	// The CSR owner is set to the ImageBuild resource for traceability
	credential, status := credentialGenerator.GenerateEnrollmentCredential(ctx, orgID, name, api.ImageBuildKind, imageBuildName)
	if err := service.ApiStatusToErr(status); err != nil {
		return nil, fmt.Errorf("generating enrollment credential: %w", err)
	}

	// Convert to agent config.yaml format
	agentConfig, err := credential.ToAgentConfig()
	if err != nil {
		return nil, fmt.Errorf("converting credential to agent config: %w", err)
	}

	return agentConfig, nil
}

// renderContainerfileTemplate renders the Containerfile template with the given data
func renderContainerfileTemplate(data containerfileData) (string, error) {
	tmpl, err := template.New("containerfile").Parse(containerfileTemplate)
	if err != nil {
		return "", fmt.Errorf("failed to parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("failed to execute template: %w", err)
	}

	return buf.String(), nil
}

// podmanWorker holds information about a running podman worker container
type podmanWorker struct {
	ContainerName       string
	TmpDir              string
	TmpOutDir           string
	TmpContainerStorage string
	Cleanup             func()
	statusUpdater       *statusUpdater // Reference to status updater for output reporting
}

// statusWriter is a thread-safe writer that captures output to a buffer
// and streams it to the status updater for progress tracking
type statusWriter struct {
	mu            sync.Mutex
	buf           *bytes.Buffer
	statusUpdater *statusUpdater
}

// Write implements io.Writer to handle the stream safely
func (w *statusWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 1. Capture to memory buffer
	w.buf.Write(p)

	// 2. Stream to updater
	if w.statusUpdater != nil {
		w.statusUpdater.reportOutput(p)
	}
	return len(p), nil
}

// runInWorker runs a podman command inside the worker container
// It streams output to the status updater to track progress
func (w *podmanWorker) runInWorker(ctx context.Context, log logrus.FieldLogger, phaseName string, args ...string) error {
	// We use "podman exec" to run inside the running container
	execArgs := append([]string{"exec", w.ContainerName, "podman"}, args...)
	cmd := exec.CommandContext(ctx, "podman", execArgs...)

	// Create a shared buffer for the final output
	var outputBuffer bytes.Buffer

	// Create our thread-safe writer
	writer := &statusWriter{
		buf:           &outputBuffer,
		statusUpdater: w.statusUpdater,
	}

	// Assign the SAME writer to both stdout and stderr.
	cmd.Stdout = writer
	cmd.Stderr = writer

	// Run() handles the starting, streaming, and waiting automatically.
	if err := cmd.Run(); err != nil {
		output := outputBuffer.String()
		log.Debugf("%s output:\n%s", phaseName, output)
		return fmt.Errorf("%s failed: %w. Output: %s", phaseName, err, output)
	}

	output := outputBuffer.String()
	log.Debugf("%s output:\n%s", phaseName, output)
	return nil
}

// startPodmanWorker starts a detached podman worker container for building images.
// It returns the container name, worker info, and a cleanup function.
func (c *Consumer) startPodmanWorker(
	ctx context.Context,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	statusUpdater *statusUpdater,
	log logrus.FieldLogger,
) (*podmanWorker, error) {
	// Check context before starting work
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Use podman image from config (defaults are applied during config loading)
	if c.cfg == nil || c.cfg.ImageBuilderWorker == nil {
		return nil, fmt.Errorf("config or ImageBuilderWorker config is nil")
	}
	podmanImage := c.cfg.ImageBuilderWorker.PodmanImage

	// Create temporary directories for the worker
	tmpDir, err := os.MkdirTemp("", "imagebuild-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}

	tmpOutDir, err := os.MkdirTemp("", "imagebuild-out-*")
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, fmt.Errorf("failed to create temporary output directory: %w", err)
	}

	tmpContainerStorage, err := os.MkdirTemp("", "imagebuild-storage-*")
	if err != nil {
		os.RemoveAll(tmpDir)
		os.RemoveAll(tmpOutDir)
		return nil, fmt.Errorf("failed to create temporary container storage directory: %w", err)
	}

	// Container paths
	containerBuildDir := "/build"
	containerOutDir := "/output"
	containerStorageDir := "/var/lib/containers"

	// Generate a unique container name so we can reference it easily
	imageBuildName := lo.FromPtr(imageBuild.Metadata.Name)
	containerName := fmt.Sprintf("build-worker-%s-%s", orgID.String()[:8], imageBuildName)

	// Start the worker container in detached mode
	log.Info("Starting worker container")
	startArgs := []string{
		"run", "-d", "--rm", // -d for detached, --rm to clean up when killed
		"--name", containerName,
		"--security-opt", "seccomp=unconfined",
		"--device", "/dev/fuse:rw",
		"--security-opt", "label=disable",
		"--env", "STORAGE_DRIVER=fuse-overlayfs",
		"--net=host",
		"-v", fmt.Sprintf("%s:%s:Z", tmpDir, containerBuildDir),
		"-v", fmt.Sprintf("%s:%s:Z", tmpOutDir, containerOutDir),
		"-v", fmt.Sprintf("%s:%s:Z", tmpContainerStorage, containerStorageDir),

		podmanImage,
		"sleep", "infinity",
	}

	if out, err := exec.CommandContext(ctx, "podman", startArgs...).CombinedOutput(); err != nil {
		os.RemoveAll(tmpDir)
		os.RemoveAll(tmpOutDir)
		os.RemoveAll(tmpContainerStorage)
		return nil, fmt.Errorf("failed to start worker: %w, output: %s", err, string(out))
	}

	cleanup := func() {
		log.Debug("Cleaning up worker container")
		exec.Command("podman", "kill", containerName).Run()
		os.RemoveAll(tmpDir)
		os.RemoveAll(tmpOutDir)
		os.RemoveAll(tmpContainerStorage)
	}

	return &podmanWorker{
		ContainerName:       containerName,
		TmpDir:              tmpDir,
		TmpOutDir:           tmpOutDir,
		TmpContainerStorage: tmpContainerStorage,
		Cleanup:             cleanup,
		statusUpdater:       statusUpdater,
	}, nil
}

// buildImageWithPodman builds the image using podman in a container-in-container setup.
// It creates a manifest list, builds for AMD64 platform, and handles authentication.
func (c *Consumer) buildImageWithPodman(
	ctx context.Context,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	containerfileResult *ContainerfileResult,
	podmanWorker *podmanWorker,
	log logrus.FieldLogger,
) error {
	// Check context before starting work
	if err := ctx.Err(); err != nil {
		return err
	}

	spec := imageBuild.Spec

	// Get destination repository URL
	destRegistryURL, err := c.getRepositoryURL(ctx, orgID, spec.Destination.Repository)
	if err != nil {
		return fmt.Errorf("failed to get destination repository URL: %w", err)
	}

	// Build the full image reference
	imageRef := fmt.Sprintf("%s/%s:%s", destRegistryURL, spec.Destination.ImageName, spec.Destination.Tag)

	// Determine platform from ImageBuild status architecture, default to linux/amd64
	platform := "linux/amd64"
	if imageBuild.Status != nil && imageBuild.Status.Architecture != nil && *imageBuild.Status.Architecture != "" {
		platform = *imageBuild.Status.Architecture
	}

	log.WithFields(logrus.Fields{
		"imageRef": imageRef,
		"platform": platform,
	}).Info("Starting podman build")

	// Write Containerfile to temporary directory
	containerfilePath := filepath.Join(podmanWorker.TmpDir, "Containerfile")
	if err := os.WriteFile(containerfilePath, []byte(containerfileResult.Containerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Containerfile: %w", err)
	}

	// Get source repository credentials for pulling the base image (FROM)
	repo, err := c.mainStore.Repository().Get(ctx, orgID, spec.Source.Repository)
	if err != nil {
		return fmt.Errorf("failed to load source repository: %w", err)
	}

	ociSpec, err := repo.Spec.AsOciRepoSpec()
	if err != nil {
		return fmt.Errorf("failed to parse OCI repository spec: %w", err)
	}

	// Build credentials string for podman (username:password format)
	// These credentials are used to pull the base image during build
	var credsFlag []string
	if ociSpec.OciAuth != nil {
		dockerAuth, err := ociSpec.OciAuth.AsDockerAuth()
		if err == nil && dockerAuth.Username != "" && dockerAuth.Password != "" {
			creds := fmt.Sprintf("%s:%s", dockerAuth.Username, dockerAuth.Password)
			credsFlag = []string{"--creds", creds}
			log.Debug("Using source repository credentials for pulling base image")
		}
	}

	// Container paths
	containerBuildDir := "/build"
	containerContainerfilePath := filepath.Join(containerBuildDir, "Containerfile")

	// ---------------------------------------------------------
	// PHASE 1: BUILD (Manifest + Build)
	// ---------------------------------------------------------
	log.Info("Phase: Build Started")

	// A. Create Manifest (ignore error if it already exists)
	if err := podmanWorker.runInWorker(ctx, log, "manifest create", "manifest", "create", imageRef); err != nil {
		// Manifest might already exist, which is okay
		if !strings.Contains(err.Error(), "already exists") {
			return err
		}
		log.Debug("Manifest list already exists, continuing")
	}

	// B. Build
	buildArgs := []string{
		"build",
		"--platform", platform,
		"--manifest", imageRef,
		"-f", containerContainerfilePath,
	}
	buildArgs = append(buildArgs, credsFlag...)
	buildArgs = append(buildArgs, containerBuildDir)

	if err := podmanWorker.runInWorker(ctx, log, "build", buildArgs...); err != nil {
		return err
	}

	log.Info("Phase: Build Completed")

	return nil
}

// pushImageWithPodman pushes the built image to the destination registry.
// It returns the image reference that was pushed.
func (c *Consumer) pushImageWithPodman(
	ctx context.Context,
	orgID uuid.UUID,
	imageBuild *api.ImageBuild,
	podmanWorker *podmanWorker,
	log logrus.FieldLogger,
) (string, error) {
	// Check context before starting work
	if err := ctx.Err(); err != nil {
		return "", err
	}

	spec := imageBuild.Spec

	// Get destination repository URL and construct image reference
	destRegistryURL, err := c.getRepositoryURL(ctx, orgID, spec.Destination.Repository)
	if err != nil {
		return "", fmt.Errorf("failed to get destination repository URL: %w", err)
	}
	imageRef := fmt.Sprintf("%s/%s:%s", destRegistryURL, spec.Destination.ImageName, spec.Destination.Tag)

	// Get repository credentials for authentication
	repo, err := c.mainStore.Repository().Get(ctx, orgID, spec.Destination.Repository)
	if err != nil {
		return "", fmt.Errorf("failed to load destination repository: %w", err)
	}

	ociSpec, err := repo.Spec.AsOciRepoSpec()
	if err != nil {
		return "", fmt.Errorf("failed to parse OCI repository spec: %w", err)
	}

	// ---------------------------------------------------------
	// PHASE: PUSH (Explicit State Transition)
	// ---------------------------------------------------------
	log.Info("Phase: Push Started")

	// A. Login (if needed inside the container)
	if ociSpec.OciAuth != nil {
		dockerAuth, err := ociSpec.OciAuth.AsDockerAuth()
		if err == nil && dockerAuth.Username != "" && dockerAuth.Password != "" {
			loginArgs := []string{
				"login",
				"-u", dockerAuth.Username,
				"-p", dockerAuth.Password,
				destRegistryURL,
			}
			if err := podmanWorker.runInWorker(ctx, log, "login", loginArgs...); err != nil {
				return "", fmt.Errorf("failed to login to registry: %w", err)
			}
		}
	}

	// B. Push
	// Note: We push the MANIFEST (imageRef), which pushes all layers
	if err := podmanWorker.runInWorker(ctx, log, "push", "push", imageRef); err != nil {
		return "", err
	}

	log.WithField("imageRef", imageRef).Info("Phase: Push Completed - Image pushed successfully")

	return imageRef, nil
}
