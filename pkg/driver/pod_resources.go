/*
Copyright 2024 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package driver

import (
	"context"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	api "k8s.io/kubelet/pkg/apis/podresources/v1"
)

const (
	defaultPodResourcesSocketPath = "/var/lib/kubelet/pod-resources/kubelet.sock"
	maxSize                       = 1024 * 1024 * 16
)

// PodResourceClientOption is an option for the client.
type PodResourceClientOption func(*PodResourceClient)

// PodResourceClient is Pod Resources API client.
type PodResourceClient struct {
	socketPath string
	conn       *grpc.ClientConn
	cli        api.PodResourcesListerClient
}

// PodResources contains resources for a pod.
type PodResources struct {
	*api.PodResources
}

// NewPodLevelResourcesClient creates a new Pod Resources API client with the given options.
func NewPodLevelResourcesClient(options ...PodResourceClientOption) (*PodResourceClient, error) {
	c := &PodResourceClient{
		socketPath: defaultPodResourcesSocketPath,
	}

	for _, o := range options {
		o(c)
	}

	if c.conn == nil {
		conn, err := grpc.NewClient("unix://"+c.socketPath,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(maxSize)),
		)
		if err != nil {
			return nil, fmt.Errorf("failed to connect podresource client: %w", err)
		}
		c.conn = conn
	}

	c.cli = api.NewPodResourcesListerClient(c.conn)
	return c, nil
}

// Close closes the client.
func (c *PodResourceClient) Close() {
	if c != nil && c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
	c.cli = nil
}

// HasClient returns true if the client has a usable client.
func (c *PodResourceClient) HasClient() bool {
	return c != nil && c.cli != nil
}

// Get queries the given pod's resources.
func (c *PodResourceClient) Get(ctx context.Context, namespace, pod string) (*PodResources, error) {
	if !c.HasClient() {
		return nil, nil
	}
	reply, err := c.cli.Get(ctx, &api.GetPodResourcesRequest{
		PodNamespace: namespace,
		PodName:      pod,
	})
	if err != nil {
		return nil, err
	}
	return &PodResources{reply.GetPodResources()}, nil
}
