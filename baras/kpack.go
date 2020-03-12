package baras

import (
	"fmt"

	"github.com/cloudfoundry/capi-bara-tests/helpers/skip_messages"

	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/helpers"
	. "github.com/cloudfoundry/capi-bara-tests/bara_suite_helpers"
	"github.com/cloudfoundry/capi-bara-tests/helpers/assets"
	"github.com/cloudfoundry/capi-bara-tests/helpers/random_name"
	. "github.com/cloudfoundry/capi-bara-tests/helpers/v3_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"
)

var _ = Describe("Kpack lifecycle", func() {
	var (
		appName     string
		appGUID     string
		token       string
		dropletGUID string
		droplet     Droplet
	)

	BeforeEach(func() {
		if !Config.GetIncludeKpack() {
			Skip(skip_messages.SkipKpackMessage)
		}

		appName = random_name.BARARandomName("APP")
		spaceName := TestSetup.RegularUserContext().Space
		spaceGUID := GetSpaceGuidFromName(spaceName)
		domainGUID := GetDomainGUIDFromName(Config.GetAppsDomain())

		By("Creating an app")
		appGUID = CreateApp(appName, spaceGUID, `{"foo":"bar"}`)
		CreateAndMapRoute(appGUID, spaceGUID, domainGUID, appName)
	})

	AfterEach(func() {
		DeleteApp(appGUID)
	})

	Context("When creating a build with the kpack lifecycle", func() {
		It("stages and starts the app successfully", func() {
			By("Creating an App and package")

			packageGUID := CreatePackage(appGUID)

			uploadURL := fmt.Sprintf("%s%s/v3/packages/%s/upload", Config.Protocol(), Config.GetApiEndpoint(), packageGUID)
			token = GetAuthToken()
			By("Uploading a Package")
			UploadPackage(uploadURL, assets.NewAssets().CatnipZip, token)

			WaitForPackageToBeReady(packageGUID)

			By("Creating a Build")
			buildGUID := StageKpackPackage(packageGUID)
			WaitForBuildToStage(buildGUID)

			dropletGUID = GetDropletFromBuild(buildGUID)

			droplet = GetDroplet(dropletGUID)
			Expect(droplet.State).To(Equal("STAGED"))
			Expect(droplet.Lifecycle.Type).To(Equal("docker"))
			Expect(droplet.Image).ToNot(BeEmpty())

			AssignDropletToApp(appGUID, dropletGUID)
			session := cf.Cf("start", appName)

			Eventually(session).Should(gexec.Exit(0))
			// Note: we'd like to use the CurlAppRoot helper but cf4k8s does not yet support https traffic to apps
			// https://github.com/cloudfoundry/cf-for-k8s/issues/46
			curl := helpers.Curl(Config, "-s", fmt.Sprintf("http://%s.%s", appName, Config.GetAppsDomain())).Wait()
			Eventually(curl).Should(gexec.Exit(0))
			Eventually(curl).Should(gbytes.Say("Catnip?"))
		})
	})
})