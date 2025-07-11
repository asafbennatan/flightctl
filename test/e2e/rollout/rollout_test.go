package rollout_test

import (
	"context"
	"fmt"

	api "github.com/flightctl/flightctl/api/v1alpha1"
	"github.com/flightctl/flightctl/test/harness/e2e"
	testutil "github.com/flightctl/flightctl/test/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
)

var _ = Describe("Rollout Policies", func() {
	var (
		ctx context.Context
		tc  *TestContext
	)

	BeforeEach(func() {
		// Initialize the test context
		ctx = testutil.StartSpecTracerForGinkgo(suiteCtx)
		tc = setupTestContext(ctx)
	})

	AfterEach(func() {
		// Cleanup the test context
		err := tc.cleanup()
		Expect(err).ToNot(HaveOccurred(), "Failed to clean up test context")
	})

	Context("Multi Device Selection", Label("79648"), func() {
		It("should select devices correctly based on BatchSequence strategy", func() {
			By("create a fleet and Enroll devices into the fleet")

			bsq1 := api.BatchSequence{
				Sequence: &[]api.Batch{
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit: intLimit(1),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit: percentageLimit("50%"),
					},
				},
			}

			labelsList := []map[string]string{
				{labelSite: siteMadrid},
				{labelSite: siteMadrid},
				{labelSite: siteMadrid},
				{labelSite: siteParis},
			}

			err := tc.setupFleetAndDevices(4, labelsList)
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err := tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			//Update fleet with template
			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(bsq1, lo.ToPtr(api.Percentage("50%")), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the first batch selects 1 device")

			tc.harness.WaitForBatchStart(fleetName, 0)

			By("Verifying the first batch selects 1 device")
			selectedDevices, err := tc.harness.GetSelectedDevicesForBatch(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(selectedDevices)).To(Equal(1))
			Expect((*selectedDevices[0].Metadata.Labels)[labelSite]).To(Equal(siteMadrid))

			tc.harness.WaitForBatchStart(fleetName, 1)

			By("Verifying the second batch selects 50% of remaining devices")
			selectedDevices, err = tc.harness.GetSelectedDevicesForBatch(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(selectedDevices)).To(Equal(1))
			Expect((*selectedDevices[0].Metadata.Labels)[labelSite]).To(Equal(siteMadrid))

			Expect(err).ToNot(HaveOccurred())
			By("Here we expect remaining 2 devices to be selected")
			tc.harness.WaitForBatchStart(fleetName, 2)
			selectedDevices, err = tc.harness.GetSelectedDevicesForBatch(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(selectedDevices)).To(Equal(2))

			By("Verifying the application is updated on all devices")
			tc.harness.WaitForBatchStart(fleetName, 3)
			err = tc.verifyAllDevicesUpdated(4)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Rollout Disruption Budget", Label("79649"), func() {
		It("should enforce the rollout disruption budget during rollouts", func() {
			By("creating a fleet with disruption budget")

			labelsList := []map[string]string{
				{labelSite: siteMadrid, labelFunction: functionWeb},
				{labelSite: siteMadrid, labelFunction: functionDb},
				{labelSite: siteMadrid, labelFunction: functionWeb},
			}

			err := tc.setupFleetAndDevices(3, labelsList)
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err := tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			fleetSpec := createFleetSpecWithoutDeviceSelection(lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec)
			fleetSpec.RolloutPolicy.DisruptionBudget = createDisruptionBudget(2, 2, []string{})

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, fleetSpec)
			Expect(err).ToNot(HaveOccurred())

			newRenderedVersion, err := tc.harness.PrepareNextDeviceVersion(tc.deviceIDs[0])
			Expect(err).ToNot(HaveOccurred())

			By("Verifying that the disruption budget is respected")
			// Get unavailable devices per group
			unavailableDevices, err := tc.harness.GetUnavailableDevicesPerGroup(fleetName, []string{labelSite, labelFunction})
			Expect(err).ToNot(HaveOccurred())

			for _, group := range unavailableDevices {
				Expect(len(group)).To(BeNumerically("<=", 1), "Should have at most 1 unavailable device per group")
			}

			for _, deviceID := range tc.deviceIDs {
				err = tc.harness.WaitForDeviceNewRenderedVersion(deviceID, newRenderedVersion)
				Expect(err).ToNot(HaveOccurred())
			}

			By("Verifying all devices are eventually updated")
			err = tc.verifyAllDevicesUpdated(3)
			Expect(err).ToNot(HaveOccurred())

			By("Checking Disruption Budget by Grouping Devices")
			err = tc.updateAppVersion("v2")
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err = tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			fleetSpec = createFleetSpecWithoutDeviceSelection(lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec)
			fleetSpec.RolloutPolicy.DisruptionBudget = createDisruptionBudget(2, 0, []string{labelSite})

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, fleetSpec)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying that the disruption budget is respected")
			// Get unavailable devices per group
			unavailableDevices, err = tc.harness.GetUnavailableDevicesPerGroup(fleetName, []string{labelSite, labelFunction})
			Expect(err).ToNot(HaveOccurred())

			for _, group := range unavailableDevices {
				Expect(len(group)).To(BeNumerically("==", 2), "Should select 2 devices in 1st batch")
			}
		})
	})

	Context("Failed Rollout", Label("79650"), func() {
		It("should pause the rollout if the success threshold is not met", func() {
			By("creating a fleet with success threshold")

			bsq2 := api.BatchSequence{
				Sequence: &[]api.Batch{
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit: intLimit(1),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteParis},
						},
						Limit: intLimit(1),
					},
				},
			}

			labelsList := []map[string]string{
				{labelSite: siteMadrid},
				{labelSite: siteParis},
			}

			err := tc.setupFleetAndDevices(2, labelsList)
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err := tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			deviceVersions := make(map[string]int)
			for _, deviceID := range tc.deviceIDs {
				deviceVersions[deviceID], err = tc.harness.PrepareNextDeviceVersion(deviceID)
				Expect(err).ToNot(HaveOccurred())
			}

			By("Simulating a failure in the first batch")
			err = tc.harness.SimulateNetworkFailure()
			Expect(err).ToNot(HaveOccurred())

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(bsq2, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			tc.harness.WaitForBatchStart(fleetName, 0)

			err = tc.harness.WaitForFleetUpdateToFail(fleetName)
			Expect(err).ToNot(HaveOccurred())

			By("Verifying that the rollout is paused due to unmet success threshold")
			rolloutStatus, err := tc.harness.GetRolloutStatus(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(rolloutStatus.Status).To(Equal(api.ConditionStatusFalse), "Rollout Update Status should be false")
			Expect(rolloutStatus.Reason).To(Equal(api.RolloutSuspendedReason), "Rollout should be paused when success threshold is not met")

			By("Fixing the failed device and verifying the rollout continues")
			err = tc.harness.FixNetworkFailure()
			Expect(err).ToNot(HaveOccurred())

			// Wait for rollout to continue
			By("Verifying that the rollout is resumed")
			err = tc.harness.WaitForDeviceNewRenderedVersion(tc.deviceIDs[0], deviceVersions[tc.deviceIDs[0]])
			Expect(err).ToNot(HaveOccurred())

			By("Verifying the first device is updated")
			// Verify that the first device is updated
			updatedDevices, err := tc.harness.GetUpdatedDevices(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(updatedDevices)).To(Equal(1), "Only One device should be updated")

			By("Verifying a new rollout can be triggered")
			deviceVersions[tc.deviceIDs[0]], err = tc.harness.PrepareNextDeviceVersion(tc.deviceIDs[0])
			Expect(err).ToNot(HaveOccurred())

			// update Name of app version in device spec to trigger a new rollout
			err = tc.updateAppVersion("v2")
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err = tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(bsq2, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			// Wait for all devices to be updated
			for _, deviceID := range tc.deviceIDs {
				err = tc.harness.WaitForDeviceNewRenderedVersion(deviceID, deviceVersions[deviceID])
				Expect(err).ToNot(HaveOccurred())
			}

			err = tc.verifyAllDevicesUpdated(2)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Template Version Update During Rollout", Label("79651"), func() {
		It("should handle updating template version during an active rollout", func() {
			By("creating a fleet with batch sequence rollout policy")

			bsq3 := api.BatchSequence{
				Sequence: &[]api.Batch{
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit: intLimit(1),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteParis},
						},
						Limit: intLimit(1),
					},
				},
			}

			labelsList := []map[string]string{
				{labelSite: siteMadrid},
				{labelSite: siteParis},
			}

			err := tc.setupFleetAndDevices(2, labelsList)
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err := tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(bsq3, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the first batch to start rolling out")
			tc.harness.WaitForBatchStart(fleetName, 1)

			By("Updating the fleet template version during rollout")
			err = tc.updateAppVersion("v2")
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err = tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(bsq3, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			newRenderedVersion, err := tc.harness.PrepareNextDeviceVersion(tc.deviceIDs[0])
			Expect(err).ToNot(HaveOccurred())

			By("Verifying that devices ae updated to new template version of the updated template")
			for _, deviceID := range tc.deviceIDs {
				err = tc.harness.WaitForDeviceNewRenderedVersion(deviceID, newRenderedVersion)
				Expect(err).ToNot(HaveOccurred())
			}

			err = tc.verifyAllDevicesUpdated(2)
			Expect(err).ToNot(HaveOccurred())
		})
	})

	Context("Changing Rollout Flow During Application Upgrade", Label("79652"), func() {
		It("should handle changes to batch sequence during an application rollout", func() {
			By("creating a fleet with initial batch sequence rollout policy")

			// Define the initial batch sequence with minimum required batches
			initialBatchSequence := api.BatchSequence{
				Sequence: &[]api.Batch{
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit:            intLimit(1),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteRome},
						},
						Limit:            intLimit(1),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{},
						},
						Limit:            percentageLimit(SuccessThreshold),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
				},
			}

			labelsList := []map[string]string{
				{labelSite: siteMadrid},
				{labelSite: siteParis},
				{labelSite: siteRome},
			}

			err := tc.setupFleetAndDevices(3, labelsList)
			Expect(err).ToNot(HaveOccurred())

			deviceSpec, err := tc.createDeviceSpec()
			Expect(err).ToNot(HaveOccurred())

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(initialBatchSequence, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the first batch to start rolling out")
			tc.harness.WaitForBatchStart(fleetName, 1)

			rolloutStatus, err := tc.harness.GetRolloutStatus(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(rolloutStatus.Status).To(Equal(api.ConditionStatusTrue), "A new rollout should be active")

			updatedBatchSequence := api.BatchSequence{
				Sequence: &[]api.Batch{
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteParis},
						},
						Limit:            intLimit(1),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteRome},
						},
						Limit:            intLimit(1),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
					{
						Selector: &api.LabelSelector{
							MatchLabels: &map[string]string{labelSite: siteMadrid},
						},
						Limit:            intLimit(1),
						SuccessThreshold: lo.ToPtr(api.Percentage(SuccessThreshold)),
					},
				},
			}

			err = tc.harness.CreateOrUpdateTestFleet(fleetName, createFleetSpec(updatedBatchSequence, lo.ToPtr(api.Percentage(SuccessThreshold)), deviceSpec))
			Expect(err).ToNot(HaveOccurred())

			By("Waiting for the first batch of the new flow to complete")
			tc.harness.WaitForBatchStart(fleetName, 0)

			// Verify the Paris device is updated first in the new flow
			selectedDevices, err := tc.harness.GetSelectedDevicesForBatch(fleetName)
			Expect(err).ToNot(HaveOccurred())
			Expect(len(selectedDevices)).To(Equal(1))
			Expect((*selectedDevices[0].Metadata.Labels)[labelSite]).To(Equal(siteParis))
		})
	})
})

const (
	fleetName        = "rollout"
	siteMadrid       = "madrid"
	siteParis        = "paris"
	siteRome         = "rome"
	functionWeb      = "web"
	functionDb       = "db"
	labelSite        = "site"
	labelFunction    = "function"
	SuccessThreshold = "100%"
)

var testFleetSelector = api.LabelSelector{
	MatchLabels: &map[string]string{"fleet": fleetName},
}

func percentageLimit(p api.Percentage) *api.Batch_Limit {
	ret := &api.Batch_Limit{}
	Expect(ret.FromPercentage(p)).ToNot(HaveOccurred())
	return ret
}

func intLimit(i int) *api.Batch_Limit {
	ret := &api.Batch_Limit{}
	Expect(ret.FromBatchLimit1(i)).ToNot(HaveOccurred())
	return ret
}

func rolloutDeviceSelection(b api.BatchSequence) *api.RolloutDeviceSelection {
	ret := &api.RolloutDeviceSelection{}
	Expect(ret.FromBatchSequence(b)).ToNot(HaveOccurred())
	return ret
}

func createFleetSpec(b api.BatchSequence, threshold *api.Percentage, testFleetSpec api.DeviceSpec) api.FleetSpec {
	return api.FleetSpec{
		RolloutPolicy: &api.RolloutPolicy{
			DefaultUpdateTimeout: lo.ToPtr("90s"),
			DeviceSelection:      rolloutDeviceSelection(b),
			SuccessThreshold:     threshold,
		},
		Selector: &testFleetSelector,
		Template: struct {
			Metadata *api.ObjectMeta "json:\"metadata,omitempty\""
			Spec     api.DeviceSpec  "json:\"spec\""
		}{
			Spec: testFleetSpec,
		},
	}
}

func createFleetSpecWithoutDeviceSelection(threshold *api.Percentage, testFleetSpec api.DeviceSpec) api.FleetSpec {
	return api.FleetSpec{
		RolloutPolicy: &api.RolloutPolicy{
			SuccessThreshold: threshold,
		},
		Selector: &testFleetSelector,
		Template: struct {
			Metadata *api.ObjectMeta "json:\"metadata,omitempty\""
			Spec     api.DeviceSpec  "json:\"spec\""
		}{
			Spec: testFleetSpec,
		},
	}
}

func createDisruptionBudget(maxUnavailable, minAvailable int, groupBy []string) *api.DisruptionBudget {
	return &api.DisruptionBudget{
		GroupBy:        &groupBy,
		MaxUnavailable: lo.ToPtr(maxUnavailable),
		MinAvailable:   lo.ToPtr(minAvailable),
	}
}

// TestContext encapsulates common test setup and configuration
type TestContext struct {
	harness           *e2e.Harness
	deviceIDs         []string
	applicationSpec   api.ApplicationProviderSpec
	applicationConfig api.ImageApplicationProviderSpec
	sleepAppImage     string
}

func setupTestContext(ctx context.Context) *TestContext {
	harness := e2e.NewTestHarness(ctx)
	extIP := harness.RegistryEndpoint()
	sleepAppImage := fmt.Sprintf("%s/sleep-app:v1", extIP)

	applicationConfig := api.ImageApplicationProviderSpec{
		Image: sleepAppImage,
	}

	appType := api.AppType("compose")

	applicationSpec := api.ApplicationProviderSpec{
		Name:    lo.ToPtr("sleepapp"),
		AppType: &appType,
	}

	return &TestContext{
		harness:           harness,
		applicationSpec:   applicationSpec,
		applicationConfig: applicationConfig,
		sleepAppImage:     sleepAppImage,
	}
}

func (tc *TestContext) setupFleetAndDevices(numDevices int, labelsList []map[string]string) error {
	err := tc.harness.CreateOrUpdateTestFleet(fleetName, testFleetSelector, api.DeviceSpec{})
	if err != nil {
		return err
	}

	tc.deviceIDs, err = tc.harness.StartMultipleVMAndEnroll(numDevices)
	if err != nil {
		return err
	}

	newRenderedVersion, err := tc.harness.PrepareNextDeviceVersion(tc.deviceIDs[0])
	if err != nil {
		return err
	}

	err = tc.harness.SetLabelsForDevicesByIndex(tc.deviceIDs, labelsList, fleetName)
	if err != nil {
		return err
	}

	for _, deviceID := range tc.deviceIDs {
		err = tc.harness.WaitForDeviceNewRenderedVersion(deviceID, newRenderedVersion)
		if err != nil {
			return err
		}
	}

	return nil
}

func (tc *TestContext) createDeviceSpec() (api.DeviceSpec, error) {
	err := tc.applicationSpec.FromImageApplicationProviderSpec(tc.applicationConfig)
	if err != nil {
		return api.DeviceSpec{}, err
	}

	return api.DeviceSpec{
		Applications: &[]api.ApplicationProviderSpec{tc.applicationSpec},
	}, nil
}

func (tc *TestContext) updateAppVersion(version string) error {
	tc.applicationSpec.Name = lo.ToPtr(fmt.Sprintf("sleepapp-%s", version))
	tc.applicationConfig.Image = fmt.Sprintf("%s/sleep-app:%s", tc.harness.RegistryEndpoint(), version)
	return tc.applicationSpec.FromImageApplicationProviderSpec(tc.applicationConfig)
}

func (tc *TestContext) verifyAllDevicesUpdated(expectedCount int) error {
	updatedDevices, err := tc.harness.GetUpdatedDevices(fleetName)
	if err != nil {
		return err
	}

	if len(updatedDevices) != expectedCount {
		return fmt.Errorf("expected %d devices to be updated, but got %d", expectedCount, len(updatedDevices))
	}

	for _, device := range updatedDevices {
		if len(device.Status.Applications) > 0 {
			app := device.Status.Applications[0]
			if app.Status != api.ApplicationStatusRunning {
				return fmt.Errorf("application %s is not running", app.Name)
			}
			if tc.applicationSpec.Name != nil && app.Name != *tc.applicationSpec.Name {
				return fmt.Errorf("device %s application name is %q, expected %q", *device.Metadata.Name, app.Name, *tc.applicationSpec.Name)
			}
		}
	}

	return nil
}

func (tc *TestContext) cleanup() error {
	tc.harness.Cleanup(true)
	return tc.harness.CleanUpAllResources()
}
