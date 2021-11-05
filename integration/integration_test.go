// This is the main file that sets up integration tests using go-check.
package integration

import (
	"bytes"
	"context"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/compose-spec/compose-go/cli"
	"github.com/compose-spec/compose-go/types"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/docker/compose/v2/pkg/api"
	"github.com/docker/compose/v2/pkg/compose"
	"github.com/docker/docker/client"

	"github.com/fatih/structs"
	"github.com/go-check/check"
	"github.com/traefik/traefik/v2/pkg/log"
	checker "github.com/vdemeester/shakers"
)

// compose "github.com/libkermit/compose/check"

var (
	integration = flag.Bool("integration", false, "run integration tests")
	container   = flag.Bool("container", false, "run container integration tests")
	host        = flag.Bool("host", false, "run host integration tests")
	showLog     = flag.Bool("tlog", false, "always show Traefik logs")
)

func Test(t *testing.T) {
	if !*integration {
		log.WithoutContext().Info("Integration tests disabled.")
		return
	}

	if *container {
		//tests launched from a container
		check.Suite(&AccessLogSuite{})
		check.Suite(&AcmeSuite{})
		check.Suite(&EtcdSuite{})
		check.Suite(&ConsulSuite{})
		check.Suite(&ConsulCatalogSuite{})
		check.Suite(&DockerComposeSuite{})
		check.Suite(&DockerSuite{})
		check.Suite(&ErrorPagesSuite{})
		check.Suite(&FileSuite{})
		check.Suite(&GRPCSuite{})
		check.Suite(&HealthCheckSuite{})
		check.Suite(&HeadersSuite{})
		check.Suite(&HostResolverSuite{})
		check.Suite(&HTTPSuite{})
		check.Suite(&HTTPSSuite{})
		check.Suite(&KeepAliveSuite{})
		check.Suite(&LogRotationSuite{})
		check.Suite(&MarathonSuite{})   //not working
		check.Suite(&MarathonSuite15{}) //not working
		check.Suite(&RateLimitSuite{})
		check.Suite(&RedisSuite{}) //not working
		check.Suite(&RestSuite{})  //not working
		check.Suite(&RetrySuite{})
		check.Suite(&SimpleSuite{}) //not working
		check.Suite(&TimeoutSuite{})
		check.Suite(&TLSClientHeadersSuite{})
		check.Suite(&TracingSuite{}) //not working
		check.Suite(&UDPSuite{})
		check.Suite(&WebsocketSuite{})
		check.Suite(&ZookeeperSuite{}) //not working
	}
	if *host {
		// tests launched from the host
		// check.Suite(&K8sSuite{})
		// check.Suite(&ProxyProtocolSuite{})
		// check.Suite(&TCPSuite{})
	}

	check.TestingT(t)
}

var traefikBinary = "../dist/traefik"

type BaseSuite struct {
	composeProject *types.Project
	dockerService  api.Service
}

func (s *BaseSuite) TearDownSuite(c *check.C) {
	// shutdown and delete compose project
	if s.composeProject != nil && s.dockerService != nil {
		// s.composeProject.Stop(c)
		err := s.dockerService.Down(context.Background(), s.composeProject.Name, api.DownOptions{})
		c.Assert(err, checker.IsNil)
	}
}

func (s *BaseSuite) createComposeProject(c *check.C, name string) {
	projectName := "integration-test-" + name
	composeFile := "resources/compose/" + name + ".yml"

	composeClient, err := client.NewClientWithOpts()
	c.Assert(err, checker.IsNil)

	s.dockerService = compose.NewComposeService(composeClient, configfile.New(composeFile))
	ops, err := cli.NewProjectOptions([]string{composeFile}, cli.WithName(projectName))
	c.Assert(err, checker.IsNil)

	s.composeProject, err = cli.ProjectFromOptions(ops)
	c.Assert(err, checker.IsNil)
	s.composeProject.Name = name
}

func withConfigFile(file string) string {
	return "--configFile=" + file
}

func (s *BaseSuite) cmdTraefik(args ...string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.Command(traefikBinary, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	return cmd, &out
}

func (s *BaseSuite) killCmd(cmd *exec.Cmd) {
	err := cmd.Process.Kill()
	if err != nil {
		log.WithoutContext().Errorf("Kill: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
}

func (s *BaseSuite) traefikCmd(args ...string) (*exec.Cmd, func(*check.C)) {
	cmd, out := s.cmdTraefik(args...)
	return cmd, func(c *check.C) {
		if c.Failed() || *showLog {
			s.displayLogK3S(c)
			s.displayTraefikLog(c, out)
		}
	}
}

func (s *BaseSuite) displayLogK3S(c *check.C) {
	filePath := "./fixtures/k8s/config.skip/k3s.log"
	if _, err := os.Stat(filePath); err == nil {
		content, errR := os.ReadFile(filePath)
		if errR != nil {
			log.WithoutContext().Error(errR)
		}
		log.WithoutContext().Println(string(content))
	}
	log.WithoutContext().Println()
	log.WithoutContext().Println("################################")
	log.WithoutContext().Println()
}

func (s *BaseSuite) displayTraefikLog(c *check.C, output *bytes.Buffer) {
	if output == nil || output.Len() == 0 {
		log.WithoutContext().Infof("%s: No Traefik logs.", c.TestName())
	} else {
		log.WithoutContext().Infof("%s: Traefik logs: ", c.TestName())
		log.WithoutContext().Infof(output.String())
	}
}

func (s *BaseSuite) getDockerHost() string {
	dockerHost := os.Getenv("DOCKER_HOST")
	if dockerHost == "" {
		// Default docker socket
		dockerHost = "unix:///var/run/docker.sock"
	}
	return dockerHost
}

func (s *BaseSuite) adaptFile(c *check.C, path string, tempObjects interface{}) string {
	// Load file
	tmpl, err := template.ParseFiles(path)
	c.Assert(err, checker.IsNil)

	folder, prefix := filepath.Split(path)
	tmpFile, err := os.CreateTemp(folder, strings.TrimSuffix(prefix, filepath.Ext(prefix))+"_*"+filepath.Ext(prefix))
	c.Assert(err, checker.IsNil)
	defer tmpFile.Close()

	model := structs.Map(tempObjects)
	model["SelfFilename"] = tmpFile.Name()

	err = tmpl.ExecuteTemplate(tmpFile, prefix, model)
	c.Assert(err, checker.IsNil)
	err = tmpFile.Sync()

	c.Assert(err, checker.IsNil)

	return tmpFile.Name()
}
