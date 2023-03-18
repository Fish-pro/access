package options

import (
	"fmt"
	"net"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	apiserveroptions "k8s.io/apiserver/pkg/server/options"
	clientset "k8s.io/client-go/kubernetes"
	clientgokubescheme "k8s.io/client-go/kubernetes/scheme"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/record"
	cliflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	logsapi "k8s.io/component-base/logs/api/v1"
	"k8s.io/component-base/metrics"
	"k8s.io/controller-manager/config"
	cmoptions "k8s.io/controller-manager/options"
	netutils "k8s.io/utils/net"

	controllerconfig "github.com/access-io/access/cmd/agent/app/config"
	ctrlmgrconfig "github.com/access-io/access/pkg/apis/config"
)

// ControllerManagerOptions is the main context object for the mca-controller-manager.
type ControllerManagerOptions struct {
	Generic *cmoptions.GenericControllerManagerConfigurationOptions

	SecureServing  *apiserveroptions.SecureServingOptionsWithLoopback
	Authentication *apiserveroptions.DelegatingAuthenticationOptions
	Authorization  *apiserveroptions.DelegatingAuthorizationOptions
	Metrics        *metrics.Options
	Logs           *logs.Options

	Master     string
	Kubeconfig string
}

// NewControllerManagerOptions creates a new ControllerManagerOptions with a default config.
func NewControllerManagerOptions() (*ControllerManagerOptions, error) {
	componentConfig, err := NewDefaultComponentConfig()
	if err != nil {
		return nil, err
	}

	s := ControllerManagerOptions{
		Generic: cmoptions.NewGenericControllerManagerConfigurationOptions(&componentConfig.Generic),

		SecureServing:  apiserveroptions.NewSecureServingOptions().WithLoopback(),
		Authentication: apiserveroptions.NewDelegatingAuthenticationOptions(),
		Authorization:  apiserveroptions.NewDelegatingAuthorizationOptions(),
		Metrics:        metrics.NewOptions(),
		Logs:           logs.NewOptions(),
	}

	// Set the PairName but leave certificate directory blank to generate in-memory by default
	s.SecureServing.ServerCert.CertDirectory = ""
	s.SecureServing.ServerCert.PairName = "application-controller-manager"
	s.SecureServing.BindPort = 10777

	s.Generic.LeaderElection.ResourceName = "mca-controller-manager"
	s.Generic.LeaderElection.ResourceNamespace = "mca-system"
	return &s, nil
}

func NewDefaultComponentConfig() (ctrlmgrconfig.ControllerManagerConfiguration, error) {
	internal := ctrlmgrconfig.ControllerManagerConfiguration{
		Generic: config.GenericControllerManagerConfiguration{
			Address:                 "0.0.0.0",
			Controllers:             []string{"*"},
			MinResyncPeriod:         metav1.Duration{Duration: 12 * time.Hour},
			ControllerStartInterval: metav1.Duration{Duration: 0 * time.Second},
		},
	}
	return internal, nil
}

// Flags returns flags for a specific APIServer by section name
func (s *ControllerManagerOptions) Flags(allControllers []string, disabledByDefaultControllers []string) cliflag.NamedFlagSets {
	fss := cliflag.NamedFlagSets{}
	s.Generic.AddFlags(&fss, allControllers, disabledByDefaultControllers)

	s.SecureServing.AddFlags(fss.FlagSet("secure serving"))
	s.Authentication.AddFlags(fss.FlagSet("authentication"))
	s.Authorization.AddFlags(fss.FlagSet("authorization"))

	s.Metrics.AddFlags(fss.FlagSet("metrics"))
	logsapi.AddFlags(s.Logs, fss.FlagSet("logs"))

	fs := fss.FlagSet("misc")
	fs.StringVar(&s.Master, "master", s.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig).")
	fs.StringVar(&s.Kubeconfig, "kubeconfig", s.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")

	return fss
}

// ApplyTo fills up controller manager config with options.
func (s *ControllerManagerOptions) ApplyTo(c *controllerconfig.Config) error {
	if err := s.Generic.ApplyTo(&c.ComponentConfig.Generic); err != nil {
		return err
	}
	if err := s.SecureServing.ApplyTo(&c.SecureServing, &c.LoopbackClientConfig); err != nil {
		return err
	}
	if s.SecureServing.BindPort != 0 || s.SecureServing.Listener != nil {
		if err := s.Authentication.ApplyTo(&c.Authentication, c.SecureServing, nil); err != nil {
			return err
		}
		if err := s.Authorization.ApplyTo(&c.Authorization); err != nil {
			return err
		}
	}
	return nil
}

// Validate is used to validate the options and config before launching the controller manager
func (s *ControllerManagerOptions) Validate(allControllers []string, disabledByDefaultControllers []string) error {
	var errs []error
	return utilerrors.NewAggregate(errs)
}

// Config return a controller manager config objective
func (s ControllerManagerOptions) Config(allControllers []string, disabledByDefaultControllers []string) (*controllerconfig.Config, error) {
	if err := s.Validate(allControllers, disabledByDefaultControllers); err != nil {
		return nil, err
	}

	if err := s.SecureServing.MaybeDefaultWithSelfSignedCerts("localhost", nil, []net.IP{netutils.ParseIPSloppy("127.0.0.1")}); err != nil {
		return nil, fmt.Errorf("error creating self-signed certificates: %v", err)
	}

	kubeconfig, err := clientcmd.BuildConfigFromFlags(s.Master, s.Kubeconfig)
	if err != nil {
		return nil, err
	}
	kubeconfig.DisableCompression = true
	kubeconfig.ContentConfig.AcceptContentTypes = s.Generic.ClientConnection.AcceptContentTypes
	kubeconfig.ContentConfig.ContentType = s.Generic.ClientConnection.ContentType
	kubeconfig.QPS = s.Generic.ClientConnection.QPS
	kubeconfig.Burst = int(s.Generic.ClientConnection.Burst)

	client, err := clientset.NewForConfig(restclient.AddUserAgent(kubeconfig, "access-agent"))
	if err != nil {
		return nil, err
	}

	eventBroadcaster := record.NewBroadcaster()
	eventRecorder := eventBroadcaster.NewRecorder(clientgokubescheme.Scheme, v1.EventSource{Component: "access-agent"})

	c := &controllerconfig.Config{
		Client:           client,
		Kubeconfig:       kubeconfig,
		EventBroadcaster: eventBroadcaster,
		EventRecorder:    eventRecorder,
	}
	if err := s.ApplyTo(c); err != nil {
		return nil, err
	}
	s.Metrics.Apply()

	return c, nil
}
