package config

import (
	"context"

	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"

	accessversioned "github.com/access-io/access/pkg/generated/clientset/versioned"
)

// Config define global options and sub controller configuration
type Config struct {
	// the general kube client
	Client *clientset.Clientset

	AClient accessversioned.Interface

	// the rest config for the master
	Kubeconfig *restclient.Config

	// the event sink
	EventRecorder record.EventRecorder
}

type completedConfig struct {
	*Config
	Ctx    context.Context
	Cancel context.CancelFunc
}

// CompletedConfig same as Config, just to swap private object.
type CompletedConfig struct {
	// Embed a private pointer that cannot be instantiated outside of this package.
	*completedConfig
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (c *Config) Complete() *CompletedConfig {
	ctx, cancel := context.WithCancel(context.TODO())
	cc := completedConfig{Config: c, Ctx: ctx, Cancel: cancel}

	return &CompletedConfig{&cc}
}
