// Code generated by client-gen. DO NOT EDIT.

package fake

import (
	v1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	rest "k8s.io/client-go/rest"
	testing "k8s.io/client-go/testing"
)

type FakeConfigV1 struct {
	*testing.Fake
}

func (c *FakeConfigV1) Builds() v1.BuildInterface {
	return &FakeBuilds{c}
}

func (c *FakeConfigV1) Images() v1.ImageInterface {
	return &FakeImages{c}
}

// RESTClient returns a RESTClient that is used to communicate
// with API server by this client implementation.
func (c *FakeConfigV1) RESTClient() rest.Interface {
	var ret *rest.RESTClient
	return ret
}
