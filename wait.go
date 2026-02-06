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
	client, err := aerospike.NewClient(host, port)
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

	// Verify Aerospike is actually ready to accept operations by attempting a write
	// Use the default "test" namespace which is available in all Aerospike configurations
	key, err := aerospike.NewKey("test", "readiness-check", "probe")
	if err != nil {
		return false, nil
	}

	// Attempt to write - if this succeeds, Aerospike is fully ready
	err = client.PutBins(nil, key, aerospike.NewBin("ready", true))
	if err != nil {
		// If we get a connection error, Aerospike isn't ready yet
		if err.Matches(types.NO_AVAILABLE_CONNECTIONS_TO_NODE) ||
			err.Matches(types.TIMEOUT) ||
			err.Matches(types.MAX_RETRIES_EXCEEDED) {
			return false, nil
		}
		// Other errors indicate a configuration problem
		return false, fmt.Errorf("failed to write readiness probe: %w", err)
	}

	return true, nil
}
