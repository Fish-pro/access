package builder

import (
	restclient "k8s.io/client-go/rest"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	accessversioned "github.com/access-io/access/pkg/generated/clientset/versioned"
)

// AccessControllerClientBuilder allows you to get clients and configs for application controllers
type AccessControllerClientBuilder interface {
	clientbuilder.ControllerClientBuilder
	AccessClient(name string) (accessversioned.Interface, error)
	AccessClientOrDie(name string) accessversioned.Interface
}

// make sure that SimpleAccessControllerClientBuilder implements AccessControllerClientBuilder
var _ AccessControllerClientBuilder = SimpleAccessControllerClientBuilder{}

// NewSimpleAccessControllerClientBuilder creates a SimpleAccessControllerClientBuilder
func NewSimpleAccessControllerClientBuilder(config *restclient.Config) SimpleAccessControllerClientBuilder {
	return SimpleAccessControllerClientBuilder{
		clientbuilder.SimpleControllerClientBuilder{
			ClientConfig: config,
		},
	}
}

// SimpleAccessControllerClientBuilder returns a fixed client with different user agents
type SimpleAccessControllerClientBuilder struct {
	clientbuilder.SimpleControllerClientBuilder
}

// AccessClient returns a mcaversioned.Interface built from the ClientBuilder
func (b SimpleAccessControllerClientBuilder) AccessClient(name string) (accessversioned.Interface, error) {
	clientConfig, err := b.Config(name)
	if err != nil {
		return nil, err
	}
	return accessversioned.NewForConfig(clientConfig)
}

// AccessClientOrDie returns a mcaversioned.interface built from the ClientBuilder with no error.
// If it gets an error getting the client, it will log the error and kill the process it's running in.
func (b SimpleAccessControllerClientBuilder) AccessClientOrDie(name string) accessversioned.Interface {
	client, err := b.AccessClient(name)
	if err != nil {
		klog.Fatal(err)
	}
	return client
}
