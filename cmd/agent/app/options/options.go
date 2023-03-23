/*
Copyright 2023 The access Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package options

import (
	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	clientgokubescheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/metrics"

	"github.com/access-io/access/cmd/agent/app/config"
	"github.com/access-io/access/pkg/builder"
)

const (
	ControllerUserAgent = "access-agent"
)

// AgentOptions is the main context object for the agent controllers.
type AgentOptions struct {
	Metrics *metrics.Options
	Logs    *logs.Options

	Master     string
	Kubeconfig string

	// Device define the network device name
	Device string
}

// NewAgentOptions return all options of controller
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{
		Metrics: metrics.NewOptions(),
		Logs:    logs.NewOptions(),
	}
}

// Config return a controller config objective
func (s *AgentOptions) Config() (*config.Config, error) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.Kubeconfig)
	if err != nil {
		return nil, err
	}

	client, err := clientset.NewForConfig(restclient.AddUserAgent(kubeconfig, ControllerUserAgent))
	if err != nil {
		return nil, err
	}

	clientBuilder := builder.NewSimpleAccessControllerClientBuilder(kubeconfig)

	eventBroadcaster := record.NewBroadcaster()
	eventRecorder := eventBroadcaster.NewRecorder(clientgokubescheme.Scheme, v1.EventSource{Component: ControllerUserAgent})

	c := &config.Config{
		Client:           client,
		AClient:          clientBuilder.AccessClientOrDie(ControllerUserAgent),
		Kubeconfig:       kubeconfig,
		EventBroadcaster: eventBroadcaster,
		EventRecorder:    eventRecorder,
		Device:           s.Device,
	}

	s.Metrics.Apply()

	return c, nil
}

// Flags returns flags for a specific APIServer by section name
func (s *AgentOptions) Flags() cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}

	s.Metrics.AddFlags(fss.FlagSet("metrics"))
	logsapi.AddFlags(s.Logs, fss.FlagSet("logs"))

	fs := fss.FlagSet("misc")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	fs.StringVar(&s.Device, "device", "eth0", "The Device define the network device name(default eth0).")

	return fss
}
