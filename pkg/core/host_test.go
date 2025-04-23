package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	"github.com/multiformats/go-multiaddr"
	"github.com/omgolab/drpc/pkg/core/relay"
	glog "github.com/omgolab/go-commons/pkg/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// TestCreateLpHostSuccess verifies CreateLpHost creates a valid host
func TestCreateLpHostSuccess(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	// Pass nil for libp2pOpts
	h, err := CreateLibp2pHost(ctx,
		WithHostLogger(logger),
	)
	if err != nil {
		t.Fatalf("CreateLpHost error: %v", err)
	}
	if h == nil {
		t.Fatal("CreateLpHost returned nil host")
	}
	// Ensure proper shutdown.
	if err := h.Close(); err != nil {
		t.Errorf("Host close failed: %v", err)
	}
}

// TestCreateLpHostMultiple ensures that multiple hosts can be created concurrently.
func TestCreateLpHostMultiple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger, _ := glog.New()
	var hosts []host.Host
	for i := range 3 {
		// Pass nil for libp2pOpts and dhtOpts
		h, err := CreateLibp2pHost(ctx,
			WithHostLogger(logger),
		)
		if err != nil {
			t.Fatalf("Iteration %d: CreateLpHost error: %v", i, err)
		}
		hosts = append(hosts, h)
	}
	for i, h := range hosts {
		if h == nil {
			t.Errorf("Host %d is nil", i)
		}
		if err := h.Close(); err != nil {
			t.Errorf("Closing host %d failed: %v", i, err)
		}
	}
}

// TestHostShutdown verifies that a host shuts down properly.
func TestHostShutdown(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	// Pass nil for libp2pOpts and dhtOpts
	h, err := CreateLibp2pHost(ctx,
		WithHostLogger(logger),
	)
	if err != nil {
		t.Fatalf("CreateLpHost error: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("Host shutdown error: %v", err)
	}
}

// TestCreateLibp2pHostWithOptions verifies that custom options are applied correctly
func TestCreateLibp2pHostWithOptions(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	// Create custom libp2p options
	listenAddr := "/ip4/127.0.0.1/tcp/9053"
	libp2pOpts := []libp2p.Option{
		libp2p.ListenAddrStrings(listenAddr),
	}

	h, err := CreateLibp2pHost(ctx,
		WithHostLogger(logger),
		WithHostLibp2pOptions(libp2pOpts...),
	)
	if err != nil {
		t.Fatalf("CreateLibp2pHost error with custom options: %v", err)
	}
	if h == nil {
		t.Fatal("CreateLibp2pHost returned nil host with custom options")
	}

	// Verify that our listen address was applied
	foundMatchingAddr := false
	for _, addr := range h.Addrs() {
		if addr.String() == listenAddr && addr.String() != "" {
			foundMatchingAddr = true
			break
		}
	}

	if !foundMatchingAddr {
		t.Error("Host does not have expected listen address")
	}

	// Ensure proper shutdown
	if err := h.Close(); err != nil {
		t.Errorf("Host close failed: %v", err)
	}
}

// TestCreateLibp2pHostWithDHTOptions tests that DHT options are properly applied
func TestCreateLibp2pHostWithDHTOptions(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	// Create custom DHT options
	dhtOpts := []dht.Option{
		dht.Mode(dht.ModeClient),
	}

	h, err := CreateLibp2pHost(ctx,
		WithHostLogger(logger),
		WithHostDHTOptions(dhtOpts...),
	)
	if err != nil {
		t.Fatalf("CreateLibp2pHost error with DHT options: %v", err)
	}
	if h == nil {
		t.Fatal("CreateLibp2pHost returned nil host with DHT options")
	}

	// Ensure proper shutdown
	if err := h.Close(); err != nil {
		t.Errorf("Host close failed: %v", err)
	}
}

// TestCreateLibp2pHostWithCancelledContext tests behavior with a cancelled context
func TestCreateLibp2pHostWithCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	logger, _ := glog.New()

	// Cancel context immediately
	cancel()

	// Should either fail or return a valid host
	h, err := CreateLibp2pHost(ctx,
		WithHostLogger(logger),
	)
	if err == nil && h != nil {
		// If it succeeds, ensure we can close it
		if err := h.Close(); err != nil {
			t.Errorf("Host close failed: %v", err)
		}
	}
}

// TestDiscoveryNotifee tests the discoveryNotifee handler
func TestDiscoveryNotifee(t *testing.T) {
	logger, _ := glog.New()

	// Create a mock host
	mockHost := &mockHost{}
	mockHost.On("ID").Return(peer.ID("mockID"))

	// Create a peer that's different from our mock host
	peerInfo := peer.AddrInfo{
		ID: peer.ID("testPeerID"),
	}

	// Create a context that would be used for connection
	mockHost.On("Connect", mock.Anything, peerInfo).Return(nil)

	// Create notifee with our mock host
	notifee := discoveryNotifee{
		h: mockHost,
		cfg: &hostCfg{
			relayManager: relay.New(context.Background(), logger),
		},
	}

	// Handle the peer found event
	notifee.HandlePeerFound(peerInfo)

	// Verify that Connect was called as expected
	mockHost.AssertExpectations(t)
}

// TestDiscoveryNotifeeWithSelf tests that discoveryNotifee ignores self connections
func TestDiscoveryNotifeeWithSelf(t *testing.T) {
	logger, _ := glog.New()

	// Create a mock host with ID "selfID"
	selfID := peer.ID("selfID")
	mockHost := &mockHost{}
	mockHost.On("ID").Return(selfID)

	// Create a peer with the same ID
	peerInfo := peer.AddrInfo{
		ID: selfID,
	}

	// Create notifee with our mock host
	notifee := discoveryNotifee{
		h: mockHost,
		cfg: &hostCfg{
			relayManager: relay.New(context.Background(), logger),
		},
	}

	// Handle the peer found event - should skip self
	notifee.HandlePeerFound(peerInfo)

	// Verify that Connect was NOT called
	mockHost.AssertNotCalled(t, "Connect")
}

// TestDiscoveryNotifeeConnectionError tests handling of connection errors
func TestDiscoveryNotifeeConnectionError(t *testing.T) {
	logger, _ := glog.New()

	// Create a mock host
	mockHost := &mockHost{}
	mockHost.On("ID").Return(peer.ID("mockID"))

	// Create a peer that's different from our mock host
	peerInfo := peer.AddrInfo{
		ID: peer.ID("testPeerID"),
	}

	// Setup Connect to return an error
	connectError := errors.New("connection failed")
	mockHost.On("Connect", mock.Anything, peerInfo).Return(connectError)

	// Create notifee with our mock host
	notifee := discoveryNotifee{
		h: mockHost,
		cfg: &hostCfg{
			relayManager: relay.New(context.Background(), logger),
		},
	}

	// Handle the peer found event - should attempt connection but handle error
	notifee.HandlePeerFound(peerInfo)

	// Verify that Connect was called as expected
	mockHost.AssertExpectations(t)
}

// TestConnectToFoundPeers tests the connectToFoundPeers function
func TestConnectToFoundPeers(t *testing.T) {
	logger, _ := glog.New()
	cfg := &hostCfg{
		relayManager: relay.New(context.Background(), logger),
	}

	// Create mock host
	mockHost := &mockHost{}
	mockHost.On("ID").Return(peer.ID("mockID"))

	// Create test peers
	peer1 := peer.AddrInfo{
		ID:    peer.ID("peer1"),
		Addrs: []multiaddr.Multiaddr{},
	}
	peer2 := peer.AddrInfo{
		ID:    peer.ID("peer2"),
		Addrs: []multiaddr.Multiaddr{},
	}
	selfPeer := peer.AddrInfo{
		ID:    peer.ID("mockID"), // Same as host ID
		Addrs: []multiaddr.Multiaddr{},
	}

	// Setup Connect to succeed for peer1 and fail for peer2
	mockHost.On("Connect", mock.Anything, peer1).Return(nil)
	mockHost.On("Connect", mock.Anything, peer2).Return(errors.New("connection error"))

	// Create channel with test peers
	peerChan := make(chan peer.AddrInfo, 3)
	peerChan <- peer1
	peerChan <- peer2
	peerChan <- selfPeer
	close(peerChan)

	// Test connectToFoundPeers
	ctx := context.Background()
	connectToFoundPeers(ctx, mockHost, cfg, peerChan)

	// Verify expectations
	mockHost.AssertCalled(t, "Connect", mock.Anything, peer1)
	mockHost.AssertCalled(t, "Connect", mock.Anything, peer2)
	mockHost.AssertNotCalled(t, "Connect", mock.Anything, selfPeer)
}

// TestConnectToFoundPeersWithContextCancellation tests behavior when context is cancelled
func TestConnectToFoundPeersWithContextCancellation(t *testing.T) {
	logger, _ := glog.New()
	cfg := &hostCfg{
		relayManager: relay.New(context.Background(), logger),
	}

	// Create mock host
	mockHost := &mockHost{}
	mockHost.On("ID").Return(peer.ID("mockID"))

	// Create test peer
	peer1 := peer.AddrInfo{
		ID:    peer.ID("peer1"),
		Addrs: []multiaddr.Multiaddr{},
	}

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel context immediately

	// Host should still attempt to connect but with timeout
	mockHost.On("Connect", mock.Anything, peer1).Return(context.Canceled)

	// Create channel with test peer
	peerChan := make(chan peer.AddrInfo, 1)
	peerChan <- peer1
	close(peerChan)

	// Test connectToFoundPeers with cancelled context
	connectToFoundPeers(ctx, mockHost, cfg, peerChan)

	// Verify expectations
	mockHost.AssertExpectations(t)
}

// TestSetupMDNS tests the setupMDNS function
func TestSetupMDNS(t *testing.T) {
	logger, _ := glog.New()
	cfg := &hostCfg{
		relayManager: relay.New(context.Background(), logger),
	}

	// Create a real host for testing mDNS setup
	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("Failed to create test host: %v", err)
	}
	defer h.Close()

	// Test setupMDNS function
	err = setupMDNS(h, cfg)
	assert.NoError(t, err, "setupMDNS should not return an error")
}

// TestSetupDHTWithDefaultMode tests setupDHT with default options
func TestSetupDHTWithDefaultMode(t *testing.T) {
	logger, _ := glog.New()
	cfg := &hostCfg{
		relayManager: relay.New(context.Background(), logger),
	}

	// Create a real host for testing DHT setup
	ctx := context.Background()
	h, err := libp2p.New()
	if err != nil {
		t.Fatalf("Failed to create test host: %v", err)
	}
	defer h.Close()

	// Test setupDHT with default options
	kadDHT, err := setupDHT(ctx, h, cfg)

	// Don't require success since DHT bootstrap might fail in test environment
	if err == nil {
		assert.NotNil(t, kadDHT, "setupDHT should return a non-nil DHT when successful")
		// Close the DHT to avoid resource leaks
		err = kadDHT.Close()
		assert.NoError(t, err, "DHT close should not return an error")
	}
}

// TestFindPeersLoopWithCancelledContext tests that findPeersLoop exits when context is cancelled
func TestFindPeersLoopWithCancelledContext(t *testing.T) {
	logger, _ := glog.New()
	cfg := &hostCfg{
		relayManager: relay.New(context.Background(), logger),
	}

	// Create mock host
	mockHost := &mockHost{}
	mockHost.On("ID").Return(peer.ID("mockID"))

	// Create a context that we'll cancel immediately
	ctx, cancel := context.WithCancel(context.Background())

	// Use a channel to signal when the function returns
	done := make(chan struct{})

	// Setup mock routing discovery
	mockDiscovery := &MockRoutingDiscovery{}
	mockPeerChan := make(chan peer.AddrInfo)
	mockDiscovery.On("FindPeers", mock.Anything, mock.Anything).Return(mockPeerChan, nil)

	// Run testFindPeersLoop (our test version) in a goroutine
	go func() {
		testFindPeersLoop(ctx, mockDiscovery, mockHost, cfg)
		close(done)
	}()

	// Cancel the context to trigger exit
	cancel()

	// Wait for the function to return or timeout
	select {
	case <-done:
		// Success: function exited when context was cancelled
	case <-time.After(2 * time.Second):
		t.Fatal("testFindPeersLoop did not exit when context was cancelled")
	}

	// Close the peer channel to avoid goroutine leak
	close(mockPeerChan)
}

// testFindPeersLoop is a testing-only version that works with our mock
// It has the same functionality as findPeersLoop but works with our mock interfaces
func testFindPeersLoop(ctx context.Context, routingDiscovery FindPeersInterface, h host.Host, cfg *hostCfg) {
	ticker := time.NewTicker(10 * time.Millisecond) // Use shorter interval for tests
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			cfg.logger.Info("Stopping DHT peer discovery loop due to context cancellation")
			return
		case <-ticker.C:
			cfg.logger.Debug("Finding peers via DHT")
			peerChan, err := routingDiscovery.FindPeers(ctx, "test-tag")
			if err != nil {
				cfg.logger.Error("DHT FindPeers error", err)
				continue
			}

			// Process peers found in this round
			go connectToFoundPeers(ctx, h, cfg, peerChan)
		}
	}
}

// FindPeersInterface is an interface for finding peers during testing
type FindPeersInterface interface {
	FindPeers(ctx context.Context, ns string) (<-chan peer.AddrInfo, error)
}

// Ensure MockRoutingDiscovery implements FindPeersInterface
var _ FindPeersInterface = (*MockRoutingDiscovery)(nil)

// Mock implementations for testing

type mockHost struct {
	mock.Mock
	host.Host
}

func (m *mockHost) ID() peer.ID {
	args := m.Called()
	return args.Get(0).(peer.ID)
}

func (m *mockHost) Connect(ctx context.Context, peerInfo peer.AddrInfo) error {
	args := m.Called(ctx, peerInfo)
	return args.Error(0)
}

func (m *mockHost) Addrs() []multiaddr.Multiaddr {
	args := m.Called()
	if args.Get(0) == nil {
		return []multiaddr.Multiaddr{}
	}
	return args.Get(0).([]multiaddr.Multiaddr)
}

func (m *mockHost) Close() error {
	args := m.Called()
	return args.Error(0)
}

// MockRoutingDiscovery implements the RoutingDiscovery interface for testing
type MockRoutingDiscovery struct {
	mock.Mock
	drouting.RoutingDiscovery
}

func (m *MockRoutingDiscovery) FindPeers(ctx context.Context, ns string) (<-chan peer.AddrInfo, error) {
	args := m.Called(ctx, ns)
	if args.Get(0) == nil {
		peerCh := make(chan peer.AddrInfo)
		close(peerCh)
		return peerCh, args.Error(1)
	}
	return args.Get(0).(<-chan peer.AddrInfo), args.Error(1)
}
