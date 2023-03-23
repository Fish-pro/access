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

// AccessClient returns a versioned.Interface built from the ClientBuilder
func (b SimpleAccessControllerClientBuilder) AccessClient(name string) (accessversioned.Interface, error) {
	clientConfig, err := b.Config(name)
	if err != nil {
		return nil, err
	}
	return accessversioned.NewForConfig(clientConfig)
}

// AccessClientOrDie returns a versioned.interface built from the ClientBuilder with no error.
// If it gets an error getting the client, it will log the error and kill the process it's running in.
func (b SimpleAccessControllerClientBuilder) AccessClientOrDie(name string) accessversioned.Interface {
	client, err := b.AccessClient(name)
	if err != nil {
		klog.Fatal(err)
	}
	return client
}
