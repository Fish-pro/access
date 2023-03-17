package main

import (
	"os"

	"k8s.io/component-base/cli"
	_ "k8s.io/component-base/logs/json/register"
	_ "k8s.io/component-base/metrics/prometheus/clientgo"
	_ "k8s.io/component-base/metrics/prometheus/version"

	"github.com/access-io/access/cmd/agent/app"
	_ "github.com/access-io/access/pkg/apis/access/install"
)

func main() {
	command := app.NewAccessAgentCommand()
	code := cli.Run(command)
	os.Exit(code)
}
