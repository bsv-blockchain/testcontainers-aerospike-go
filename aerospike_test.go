package aerospike

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bsv-blockchain/aerospike-client-go/v8"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
)

// Unit tests for option functions (no Docker required)

func TestWithNamespaceOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithNamespace("custom-namespace")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, "custom-namespace", req.Env["NAMESPACE"])
}

func TestWithLogLevelOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithLogLevel("debug")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, "debug", req.Env["AEROSPIKE_LOG_LEVEL"])
}

func TestWithImageOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithImage("aerospike/aerospike-server:7.0")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, "aerospike/aerospike-server:7.0", req.Image)
}

func TestWithEnterpriseEditionOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithEnterpriseEdition()

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, "aerospike/aerospike-server-enterprise:8.0", req.Image)
}

func TestWithPortOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithPort("4000/tcp")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, []string{"4000/tcp"}, req.ExposedPorts)
}

func TestWithPortOptionOverridesExisting(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			ExposedPorts: []string{"3000/tcp"},
		},
	}
	opt := WithPort("4000/tcp")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Equal(t, []string{"4000/tcp"}, req.ExposedPorts)
}

func TestWithTTLSupportOption(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithTTLSupport("test")

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Len(t, req.LifecycleHooks, 1)
	assert.Len(t, req.LifecycleHooks[0].PostStarts, 1)
}

func TestWithTTLSupportOptionDefaultNamespace(t *testing.T) {
	req := &testcontainers.GenericContainerRequest{}
	opt := WithTTLSupport("") // empty should default to "test"

	err := opt.Customize(req)
	require.NoError(t, err)

	assert.Len(t, req.LifecycleHooks, 1)
}

// skipIfDockerNotAvailable skips the test if Docker daemon is not available.
func skipIfDockerNotAvailable(t *testing.T) {
	t.Helper()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Skipf("Docker client creation failed: %v", err)
		return
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Ping(ctx, client.PingOptions{})
	if err != nil {
		t.Skipf("Docker is not available: %v", err)
	}
}

// isTransientStartError reports whether err looks like a transient
// infrastructure failure pulling the image (e.g. Docker Hub timeouts or rate
// limits) rather than a real defect. These regularly cause spurious CI failures
// when the registry is slow or throttling, even though the container would
// start fine on retry.
func isTransientStartError(err error) bool {
	if err == nil {
		return false
	}

	transient := []string{
		"Client.Timeout",           // net/http client timeout (registry manifest fetch)
		"request canceled",         // context/transport cancellation during pull
		"i/o timeout",              // network timeout reaching the registry
		"TLS handshake timeout",    // slow registry TLS negotiation
		"connection reset by peer", // dropped connection mid-pull
		"registry-1.docker.io",     // Docker Hub registry endpoint errors
		"auth.docker.io",           // Docker Hub auth endpoint errors
		"toomanyrequests",          // Docker Hub rate limiting
		"no such host",             // transient DNS resolution failure
	}

	msg := err.Error()
	for _, s := range transient {
		if strings.Contains(msg, s) {
			return true
		}
	}

	return false
}

// startContainer starts an Aerospike container, retrying a few times when the
// failure is a transient registry/image-pull error. It fails the test on any
// non-transient error or once retries are exhausted.
func startContainer(ctx context.Context, t *testing.T, opts ...testcontainers.ContainerCustomizer) *Container {
	t.Helper()

	const maxAttempts = 3

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		container, err := RunContainer(ctx, opts...)
		if err == nil {
			return container
		}

		lastErr = err
		if !isTransientStartError(err) {
			break
		}

		t.Logf("transient error starting Aerospike container (attempt %d/%d): %v", attempt, maxAttempts, err)
		if attempt < maxAttempts {
			time.Sleep(time.Duration(attempt) * 2 * time.Second)
		}
	}

	require.NoError(t, lastErr, "failed to start Aerospike container")

	return nil
}

// newAerospikeClient connects an Aerospike client to host:port, fails the test
// if it cannot connect, and registers a cleanup that closes the client.
//
// The explicit Close (via t.Cleanup) tears the cluster down deterministically.
// Without it, the client's GC finalizer calls Close at an arbitrary time, which
// races with in-flight operations on the partition table and can rip the
// connection pool out from under a live command (caught by the -race detector).
func newAerospikeClient(t *testing.T, host string, port int) *aerospike.Client {
	t.Helper()

	client, err := aerospike.NewClient(host, port)
	require.NoErrorf(t, err, "failed to initialize Aerospike client")
	t.Cleanup(client.Close)
	require.Truef(t, client.IsConnected(), "failed to connect to Aerospike")

	return client
}

func TestPut(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container := startContainer(ctx, t, WithNamespace("namespace"))
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client := newAerospikeClient(t, host, port)

	key, err := aerospike.NewKey("namespace", "set", "key")
	require.NoErrorf(t, err, "failed to create Aerospike key")
	bin := aerospike.NewBin("bin", "value")

	err = client.PutBins(nil, key, bin)
	require.NoErrorf(t, err, "failed to create Aerospike record")
}

func TestWithImage(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	customImage := "aerospike/aerospike-server:7.2"
	container := startContainer(ctx, t, WithImage(customImage), WithNamespace("test"))
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	// newAerospikeClient fails the test if the client cannot connect.
	newAerospikeClient(t, host, port)
}

func TestWithLogLevel(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container := startContainer(ctx, t, WithNamespace("test"), WithLogLevel("debug"))
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	// newAerospikeClient fails the test if the client cannot connect.
	newAerospikeClient(t, host, port)
}

func TestWithTTLSupport(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container := startContainer(ctx, t, WithTTLSupport("test"))
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client := newAerospikeClient(t, host, port)

	t.Run("Write with explicit TTL succeeds", func(t *testing.T) {
		policy := aerospike.NewWritePolicy(0, 30) // 30 seconds TTL
		key, err := aerospike.NewKey("test", "set", "ttl-key")
		require.NoErrorf(t, err, "failed to create Aerospike key")

		bin := aerospike.NewBin("bin", "value")
		err = client.PutBins(policy, key, bin)
		require.NoErrorf(t, err, "write with explicit TTL should succeed when TTL support is enabled")
	})

	t.Run("Write with TTLDontExpire succeeds", func(t *testing.T) {
		policy := aerospike.NewWritePolicy(0, aerospike.TTLDontExpire)
		key, err := aerospike.NewKey("test", "set", "no-ttl-key")
		require.NoErrorf(t, err, "failed to create Aerospike key")

		bin := aerospike.NewBin("bin", "value")
		err = client.PutBins(policy, key, bin)
		require.NoErrorf(t, err, "write with TTLDontExpire should succeed")
	})
}

func TestPutWithEnterprise(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container := startContainer(ctx, t, WithNamespace("namespace"), WithEnterpriseEdition())

	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")

	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client := newAerospikeClient(t, host, port)

	t.Run("Put", func(t *testing.T) {
		key, err := aerospike.NewKey("namespace", "set", "key1")
		require.NoErrorf(t, err, "failed to create Aerospike key")

		bin := aerospike.NewBin("bin", "value")

		err = client.PutBins(nil, key, bin)
		require.NoErrorf(t, err, "failed to create Aerospike record")
	})

	t.Run("Put with transaction and abort", func(t *testing.T) {
		// Begin a transaction
		policy := aerospike.NewWritePolicy(0, 0)

		txn := aerospike.NewTxn()
		policy.Txn = txn

		key, err := aerospike.NewKey("namespace", "set", "key2")
		require.NoErrorf(t, err, "failed to create Aerospike key")

		bin := aerospike.NewBin("bin", "value")

		_, err = client.Get(nil, key)
		require.Error(t, err)
		require.ErrorIs(t, err, aerospike.ErrKeyNotFound)

		// Perform put operations within the transaction
		err = client.PutBins(policy, key, bin)
		if err != nil && strings.Contains(err.Error(), "UNSUPPORTED_FEATURE") {
			t.Skip("Cluster is empty, skipping transaction test")
		}

		require.NoErrorf(t, err, "failed to create Aerospike record")

		// Verify that the record exists
		value, err := client.Get(nil, key)
		require.NoError(t, err)
		assert.Equal(t, "value", value.Bins["bin"])

		// Abort the transaction by not committing
		status, err := client.Abort(txn)
		require.NoError(t, err)
		assert.Equal(t, aerospike.AbortStatusOK, status)

		// Verify that the record does not exist
		_, err = client.Get(nil, key)
		require.Error(t, err)
		require.ErrorIs(t, err, aerospike.ErrKeyNotFound)
	})
}

// Fuzz tests

func FuzzWithNamespace(f *testing.F) {
	// Seed corpus with edge cases
	f.Add("")
	f.Add("test")
	f.Add("namespace-with-dashes")
	f.Add("namespace_with_underscores")
	f.Add("\u540d\u524d\u7a7a\u9593")
	f.Add(strings.Repeat("a", 1000))
	f.Add("namespace\x00with\x00nulls")
	f.Add("namespace\nwith\nnewlines")
	f.Add("namespace\twith\ttabs")
	f.Add(" leading-and-trailing-spaces ")
	f.Add("UPPERCASE")
	f.Add("MixedCase123")

	f.Fuzz(func(t *testing.T, namespace string) {
		req := &testcontainers.GenericContainerRequest{}
		opt := WithNamespace(namespace)

		err := opt.Customize(req)
		require.NoError(t, err)
		assert.Equal(t, namespace, req.Env["NAMESPACE"])
	})
}
