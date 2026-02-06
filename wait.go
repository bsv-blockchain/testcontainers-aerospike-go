package aerospike

import (
	"context"
	"fmt"
	"time"

	"github.com/aerospike/aerospike-client-go/v8"
	"github.com/aerospike/aerospike-client-go/v8/types"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	defaultStartupTimeout = 60 * time.Second
	defaultPollInterval   = 100 * time.Millisecond
)

type aerospikeWaitStrategy struct{}

var _ wait.Strategy = (*aerospikeWaitStrategy)(nil)

func newAerospikeWaitStrategy() aerospikeWaitStrategy {
	return aerospikeWaitStrategy{}
}

func (s aerospikeWaitStrategy) WaitUntilReady(ctx context.Context, target wait.StrategyTarget) error {
	ctx, cancel := context.WithTimeout(ctx, defaultStartupTimeout)
	defer cancel()

	if err := wait.NewHostPortStrategy(aerospikeServicePort).WaitUntilReady(ctx, target); err != nil {
		return fmt.Errorf("error waiting for port to open: %w", err)
	}

	host, err := target.Host(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch host: %w", err)
	}
	port, err := target.MappedPort(ctx, aerospikeServicePort)
	if err != nil {
		return fmt.Errorf("failed to fetch port: %w", err)
	}
	return s.pollUntilReady(ctx, host, port.Int())
}

func (s aerospikeWaitStrategy) pollUntilReady(ctx context.Context, host string, port int) error {
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timed out while waiting for Aerospike to start: %w", ctx.Err())
		case <-time.After(defaultPollInterval):
			isReady, err := s.isReady(host, port)
			if err != nil {
				return err
			}
			if isReady {
				return nil
			}
		}
	}
}

func (s aerospikeWaitStrategy) isReady(host string, port int) (bool, error) {
	// This is similar to the implementation in testcontainers-spring-boot:
	// https://github.com/PlaytikaOSS/testcontainers-spring-boot/blob/0c007f0b774eaed595e029c94e812a30fe2d1a6b/embedded-aerospike/src/main/java/com/playtika/testcontainer/aerospike/AerospikeWaitStrategy.java#L23
	clientPolicy := aerospike.NewClientPolicy()
	// Set a short timeout for readiness checks to fail fast
	clientPolicy.Timeout = 2 * time.Second

	client, err := aerospike.NewClientWithPolicy(clientPolicy, host, port)
	if err != nil {
		if err.Matches(types.INVALID_NODE_ERROR) {
			return false, nil
		}
		return false, fmt.Errorf("failed to connect to Aerospike: %w", err)
	}
	defer client.Close()

	if !client.IsConnected() {
		return false, nil
	}

	// Verify cluster is ready by checking cluster stability
	// GetNodes() will return nodes only when cluster is fully initialized
	nodes := client.GetNodes()
	if len(nodes) == 0 {
		return false, nil
	}

	// Additional check: verify we can get node stats which confirms cluster is operational
	for _, node := range nodes {
		if !node.IsActive() {
			return false, nil
		}
	}

	return true, nil
}
