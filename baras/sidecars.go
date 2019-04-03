package baras

import (
	"encoding/json"
	"fmt"
	"github.com/cloudfoundry-incubator/cf-test-helpers/cf"
	"github.com/cloudfoundry-incubator/cf-test-helpers/helpers"
	. "github.com/cloudfoundry/capi-bara-tests/bara_suite_helpers"
	"github.com/cloudfoundry/capi-bara-tests/helpers/assets"
	"github.com/cloudfoundry/capi-bara-tests/helpers/random_name"
	. "github.com/cloudfoundry/capi-bara-tests/helpers/v3_helpers"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gbytes"
	. "github.com/onsi/gomega/gexec"
)

var _ = Describe("sidecars", func() {
	var (
		appName             string
		appGUID             string
		spaceGUID           string
		spaceName           string
		appRoutePrefix      string
		sidecarRoutePrefix1 string
		sidecarRoutePrefix2 string
		sidecarGUID         string
	)

	BeforeEach(func() {
		appName = random_name.BARARandomName("APP")
		spaceName = TestSetup.RegularUserContext().Space
		spaceGUID = GetSpaceGuidFromName(spaceName)

		By("Creating an App")
		appGUID = CreateApp(appName, spaceGUID, `{"WHAT_AM_I":"MOTORCYCLE"}`)
		_ = AssociateNewDroplet(appGUID, assets.NewAssets().DoraZip)
	})

	AfterEach(func() {
		FetchRecentLogs(appGUID, GetAuthToken(), Config)
		DeleteApp(appGUID)
	})

	Context("when the app has a sidecar associated with its web process", func() {
		BeforeEach(func() {
			CreateSidecar("my_sidecar1", []string{"web"}, fmt.Sprintf("WHAT_AM_I=LEFT_SIDECAR bundle exec rackup config.ru -p %d", 8081), appGUID)
			CreateSidecar("my_sidecar2", []string{"web"}, fmt.Sprintf("WHAT_AM_I=RIGHT_SIDECAR bundle exec rackup config.ru -p %d", 8082), appGUID)

			appEndpoint := fmt.Sprintf("/v2/apps/%s", appGUID)
			extraPortsJSON, err := json.Marshal(
				struct {
					Ports []int `json:"ports"`
				}{
					[]int{8080, 8081, 8082},
				},
			)
			Expect(err).NotTo(HaveOccurred())
			session := cf.Cf("curl", appEndpoint, "-X", "PUT", "-d", string(extraPortsJSON))
			Eventually(session).Should(Exit(0))

			appRoutePrefix = random_name.BARARandomName("ROUTE")
			sidecarRoutePrefix1 = random_name.BARARandomName("ROUTE")
			sidecarRoutePrefix2 = random_name.BARARandomName("ROUTE")

			CreateAndMapRouteWithPort(appGUID, spaceName, Config.GetAppsDomain(), appRoutePrefix, 8080)
			CreateAndMapRouteWithPort(appGUID, spaceName, Config.GetAppsDomain(), sidecarRoutePrefix1, 8081)
			CreateAndMapRouteWithPort(appGUID, spaceName, Config.GetAppsDomain(), sidecarRoutePrefix2, 8082)

			Eventually(session).Should(Exit(0))
		})

		Context("and the app and sidecar are listening on different ports", func() {
			It("and successfully responds on each port", func() {
				session := cf.Cf("start", appName)
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s", appRoutePrefix, Config.GetAppsDomain()))
				Eventually(session).Should(Say("Hi, I'm Dora!"))
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s/env/WHAT_AM_I", appRoutePrefix, Config.GetAppsDomain()))
				Eventually(session).ShouldNot(Say("MOTORCYCLE"))
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s/env/WHAT_AM_I", sidecarRoutePrefix1, Config.GetAppsDomain()))
				Eventually(session).Should(Say("LEFT_SIDECAR"))
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s/env/WHAT_AM_I", sidecarRoutePrefix2, Config.GetAppsDomain()))
				Eventually(session).Should(Say("RIGHT_SIDECAR"))
				Eventually(session).Should(Exit(0))
			})
		})

		Context("when the app has a sidecar that just sleeps", func() {
			BeforeEach(func() {
				sidecarGUID = CreateSidecar("my_sidecar", []string{"web"}, "sleep 100000", appGUID)
			})

			It("stops responding only after an app restart", func() {
				session := cf.Cf("start", appName)
				Eventually(session).Should(Exit(0))

				By("verify the sidecar is running")
				session = cf.Cf("ssh", appName, "-c", "ps aux | grep sleep | grep -v grep")
				Eventually(session).Should(Exit(0))

				By("deleted the sidecar")
				session = cf.Cf("curl", fmt.Sprintf("/v3/sidecars/%s", sidecarGUID), "-X", "DELETE")
				Eventually(session).Should(Exit(0))

				By("verify it still responds")
				session = cf.Cf("ssh", appName, "-c", "ps aux | grep sleep | grep -v grep")
				Eventually(session).Should(Exit(0))

				restartApp(appName)

				By("verify it no longer responds")
				session = cf.Cf("ssh", appName, "-c", "ps aux | grep sleep | grep -v grep")
				Eventually(session).Should(Exit(1))

			})
		})

		Context("and a sidecar is crashing", func() {
			It("crashes the main app/second sidecar and Diego brings it back", func() {
				session := cf.Cf("start", appName)
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s", appRoutePrefix, Config.GetAppsDomain()))
				Eventually(session).Should(Say("Hi, I'm Dora!"))
				Eventually(session).Should(Exit(0))

				By("Crashing the sidecar process")
				session = helpers.Curl(Config, fmt.Sprintf("%s.%s/sigterm/KILL", sidecarRoutePrefix1, Config.GetAppsDomain()))
				Eventually(session).Should(Say("502"))
				Eventually(session).Should(Exit(0))

				By("Polling the app and sidecar for 404s")
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s", appRoutePrefix, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("404 Not Found: Requested route"))
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s", sidecarRoutePrefix2, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("404 Not Found: Requested route"))

				By("Polling for the app to be restarted by Diego")
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s", appRoutePrefix, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("Hi, I'm Dora!"))
			})
		})

		Context("and the app is crashing", func() {
			It("crashes the sidecars as well", func() {
				session := cf.Cf("start", appName)
				Eventually(session).Should(Exit(0))

				session = helpers.Curl(Config, fmt.Sprintf("%s.%s", appRoutePrefix, Config.GetAppsDomain()))
				Eventually(session).Should(Say("Hi, I'm Dora!"))
				Eventually(session).Should(Exit(0))

				By("Crashing the main app process")
				session = helpers.Curl(Config, fmt.Sprintf("%s.%s/sigterm/KILL", appRoutePrefix, Config.GetAppsDomain()))
				Eventually(session).Should(Say("502"))
				Eventually(session).Should(Exit(0))

				By("Polling both sidecars for 404s")
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s", sidecarRoutePrefix1, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("404 Not Found: Requested route"))
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s", sidecarRoutePrefix2, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("404 Not Found: Requested route"))

				By("Polling for the sidecars to be restarted by Diego")
				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s/env/WHAT_AM_I", sidecarRoutePrefix1, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("LEFT_SIDECAR"))

				Eventually(func() *Session {
					session := helpers.Curl(Config, fmt.Sprintf("%s.%s/env/WHAT_AM_I", sidecarRoutePrefix2, Config.GetAppsDomain()))
					Eventually(session).Should(Exit(0))
					return session
				}, Config.DefaultTimeoutDuration()).Should(Say("RIGHT_SIDECAR"))
			})
		})
	})
})
