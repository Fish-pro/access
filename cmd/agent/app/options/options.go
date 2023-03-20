package options

import (
	v1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	clientgokubescheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/klog/v2"

	"github.com/access-io/access/cmd/agent/app/config"
	"github.com/access-io/access/pkg/builder"
)

const (
	ControllerUserAgent = "access-agent"
)

// AgentOptions is the main context object for the agent controllers.
type AgentOptions struct {
	Master     string
	Kubeconfig string
}

// NewAgentOptions return all options of controller
func NewAgentOptions() *AgentOptions {
	return &AgentOptions{}
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

	eventRecorder := createRecorder(client, ControllerUserAgent)

	c := &config.Config{
		Client:        client,
		AClient:       clientBuilder.AccessClientOrDie(ControllerUserAgent),
		Kubeconfig:    kubeconfig,
		EventRecorder: eventRecorder,
	}

	return c, nil
}

// Flags returns flags for a specific APIServer by section name
func (s *AgentOptions) Flags() cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}

	fs := fss.FlagSet("misc")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")

	return fss
}

// createRecorder return a event recorder
func createRecorder(kubeClient clientset.Interface, userAgent string) record.EventRecorder {
	eventBroadcaster := record.NewBroadcaster()
	eventBroadcaster.StartLogging(klog.Infof)
	eventBroadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	return eventBroadcaster.NewRecorder(clientgokubescheme.Scheme, v1.EventSource{Component: userAgent})
}
