// Package aerospike provides a testcontainer for Aerospike database.
package aerospike

import (
	"context"
	"fmt"

	"github.com/testcontainers/testcontainers-go"
)

const (
	aerospikeServicePort     = "3000/tcp"
	communityAerospikeImage  = "aerospike/aerospike-server:8.0"
	enterpriseAerospikeImage = "aerospike/aerospike-server-enterprise:8.0"
)

// Container represents a running Aerospike container.
type Container struct {
	testcontainers.Container
}

// RunContainer creates an instance of the Aerospike container type.
func RunContainer(ctx context.Context, opts ...testcontainers.ContainerCustomizer) (*Container, error) {
	containerRequest := testcontainers.ContainerRequest{
		Image:        communityAerospikeImage,
		ExposedPorts: []string{"3000/tcp"},
		WaitingFor:   newAerospikeWaitStrategy(),
	}

	genericContainerRequest := testcontainers.GenericContainerRequest{
		ContainerRequest: containerRequest,
		Started:          true,
	}

	for _, opt := range opts {
		if err := opt.Customize(&genericContainerRequest); err != nil {
			return nil, fmt.Errorf("failed to apply option: %w", err)
		}
	}

	container, err := testcontainers.GenericContainer(ctx, genericContainerRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to start Aerospike: %w", err)
	}

	return &Container{Container: container}, nil
}

// ServicePort returns the port on which the Aerospike container is listening.
func (c Container) ServicePort(ctx context.Context) (int, error) {
	port, err := c.MappedPort(ctx, aerospikeServicePort)
	if err != nil {
		return 0, err
	}
	return port.Int(), nil
}

// WithImage sets the image for the Aerospike container.
func WithImage(image string) testcontainers.CustomizeRequestOption {
	return testcontainers.WithImage(image)
}

// WithEnterpriseEdition sets the image to the enterprise edition of Aerospike.
func WithEnterpriseEdition() testcontainers.CustomizeRequestOption {
	return WithImage(enterpriseAerospikeImage)
}

// WithNamespace sets the default namespace that is created when Aerospike
// starts. By default, this is set to "test".
func WithNamespace(namespace string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if req.Env == nil {
			req.Env = make(map[string]string)
		}
		req.Env["NAMESPACE"] = namespace

		return nil
	}
}

// WithLogLevel sets the log level for the Aerospike container.
func WithLogLevel(logLevel string) testcontainers.CustomizeRequestOption {
	return func(req *testcontainers.GenericContainerRequest) error {
		if req.Env == nil {
			req.Env = make(map[string]string)
		}
		req.Env["AEROSPIKE_LOG_LEVEL"] = logLevel

		return nil
	}
}

// WithTTLSupport enables TTL (time-to-live) support for records by setting nsup-period.
// This is required for records with explicit TTL values to expire properly.
// The namespace parameter specifies which namespace to configure (default: "test").
func WithTTLSupport(namespace string) testcontainers.CustomizeRequestOption {
	if namespace == "" {
		namespace = "test"
	}
	return func(req *testcontainers.GenericContainerRequest) error {
		req.LifecycleHooks = append(req.LifecycleHooks, testcontainers.ContainerLifecycleHooks{
			PostStarts: []testcontainers.ContainerHook{
				func(ctx context.Context, c testcontainers.Container) error {
					cmd := []string{"asinfo", "-v", fmt.Sprintf("set-config:context=namespace;id=%s;nsup-period=10", namespace)}
					_, _, err := c.Exec(ctx, cmd)
					return err
				},
			},
		})
		return nil
	}
}
