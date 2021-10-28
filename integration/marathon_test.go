package integration

import (
	"context"
	"net/http"
	"os"
	"time"

	"github.com/docker/compose/v2/pkg/api"
	"github.com/gambol99/go-marathon"
	"github.com/go-check/check"
	"github.com/traefik/traefik/v2/integration/try"
	checker "github.com/vdemeester/shakers"
)

const (
	containerNameMesosSlave = "mesos-slave"
	containerNameMarathon   = "marathon"
)

// Marathon test suites (using libcompose).
type MarathonSuite struct {
	BaseSuite
	marathonURL string
}

func (s *MarathonSuite) SetUpSuite(c *check.C) {
	s.createComposeProject(c, "marathon")
	err := s.dockerService.Up(context.Background(), s.composeProject, api.UpOptions{})
	c.Assert(err, checker.IsNil)

	s.marathonURL = "http://" + containerNameMarathon + ":8080"

	// Wait for Marathon readiness prior to creating the client so that we
	// don't run into the "all cluster members down" state right from the
	// start.
	err = try.GetRequest(s.marathonURL+"/v2/leader", 1*time.Minute, try.StatusCodeIs(http.StatusOK))
	c.Assert(err, checker.IsNil)
}

func deployApplication(c *check.C, client marathon.Marathon, application *marathon.Application) {
	deploy, err := client.UpdateApplication(application, false)
	c.Assert(err, checker.IsNil)
	// Wait for deployment to complete.
	c.Assert(client.WaitOnDeployment(deploy.DeploymentID, 1*time.Minute), checker.IsNil)
}

func (s *MarathonSuite) TestConfigurationUpdate(c *check.C) {
	// Start Traefik.
	file := s.adaptFile(c, "fixtures/marathon/simple.toml", struct {
		MarathonURL string
	}{s.marathonURL})
	defer os.Remove(file)
	cmd, display := s.traefikCmd(withConfigFile(file))
	defer display(c)
	err := cmd.Start()
	c.Assert(err, checker.IsNil)
	defer s.killCmd(cmd)

	// Wait for Traefik to turn ready.
	err = try.GetRequest("http://127.0.0.1:8000/", 2*time.Second, try.StatusCodeIs(http.StatusNotFound))
	c.Assert(err, checker.IsNil)

	// Prepare Marathon client.
	config := marathon.NewDefaultConfig()
	config.URL = s.marathonURL
	client, err := marathon.NewClient(config)
	c.Assert(err, checker.IsNil)

	// Create test application to be deployed.
	app := marathon.NewDockerApplication().
		Name("/whoami").
		CPU(0.1).
		Memory(32).
		AddLabel("traefik.http.Routers.rt.Rule", "PathPrefix(`/service`)")
	app.Container.Docker.Bridged().
		Expose(80).
		Container("traefik/whoami")

	// Deploy the test application.
	deployApplication(c, client, app)

	// Query application via Traefik.
	err = try.GetRequest("http://127.0.0.1:8000/service", 30*time.Second, try.StatusCodeIs(http.StatusOK))
	c.Assert(err, checker.IsNil)

	// Create test application with services to be deployed.
	app = marathon.NewDockerApplication().
		Name("/whoami").
		CPU(0.1).
		Memory(32).
		AddLabel("traefik.http.Routers.app.Rule", "PathPrefix(`/app`)")
	app.Container.Docker.Bridged().
		Expose(80).
		Container("traefik/whoami")

	// Deploy the test application.
	deployApplication(c, client, app)

	// Query application via Traefik.
	err = try.GetRequest("http://127.0.0.1:8000/app", 30*time.Second, try.StatusCodeIs(http.StatusOK))
	c.Assert(err, checker.IsNil)
}
