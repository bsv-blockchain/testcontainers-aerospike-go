package aerospike

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aerospike/aerospike-client-go/v8"
	"github.com/docker/docker/client"
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

// skipIfDockerNotAvailable skips the test if Docker daemon is not available.
func skipIfDockerNotAvailable(t *testing.T) {
	t.Helper()

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker client creation failed: %v", err)
		return
	}
	defer func() { _ = cli.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = cli.Ping(ctx)
	if err != nil {
		t.Skipf("Docker is not available: %v", err)
	}
}

func TestPut(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container, err := RunContainer(ctx, WithNamespace("namespace"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client, err := aerospike.NewClient(host, port)
	require.NoErrorf(t, err, "failed to initialize Aerospike client")
	require.Truef(t, client.IsConnected(), "failed to connect to Aerospike")

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
	container, err := RunContainer(ctx, WithImage(customImage), WithNamespace("test"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client, err := aerospike.NewClient(host, port)
	require.NoErrorf(t, err, "failed to initialize Aerospike client")
	require.Truef(t, client.IsConnected(), "failed to connect to Aerospike")
}

func TestWithLogLevel(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container, err := RunContainer(ctx, WithNamespace("test"), WithLogLevel("debug"))
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")
	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client, err := aerospike.NewClient(host, port)
	require.NoErrorf(t, err, "failed to initialize Aerospike client")
	require.Truef(t, client.IsConnected(), "failed to connect to Aerospike")
}

func TestPutWithEnterprise(t *testing.T) {
	skipIfDockerNotAvailable(t)

	ctx := context.Background()

	container, err := RunContainer(ctx, WithNamespace("namespace"), WithEnterpriseEdition())
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoErrorf(t, container.Terminate(ctx), "failed to terminate Aerospike container")
	})

	host, err := container.Host(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike host")

	port, err := container.ServicePort(ctx)
	require.NoErrorf(t, err, "failed to fetch Aerospike port")

	client, err := aerospike.NewClient(host, port)
	require.NoErrorf(t, err, "failed to initialize Aerospike client")
	require.Truef(t, client.IsConnected(), "failed to connect to Aerospike")

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
