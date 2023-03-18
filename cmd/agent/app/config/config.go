package config

import (
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	"github.com/access-io/access/pkg/generated/clientset/versioned"
)

// Config define global options and sub controller configuration
type Config struct {
	// the general kube client
	Client *clientset.Clientset

	AClient versioned.Interface

	// the rest config for the master
	Kubeconfig *restclient.Config

	// the event sink
	EventRecorder record.EventRecorder
}

type completedConfig struct {
	*Config
}

// CompletedConfig same as Config, just to swap private object.
type CompletedConfig struct {
	// Embed a private pointer that cannot be instantiated outside of this package.
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete() *CompletedConfig {
	cc := completedConfig{c}

	return &CompletedConfig{&cc}
}