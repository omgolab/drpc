package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/omgolab/drpc/pkg/config"
	log "github.com/omgolab/go-commons/pkg/log"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	libp2pmdns "github.com/libp2p/go-libp2p/p2p/discovery/mdns"
	drouting "github.com/libp2p/go-libp2p/p2p/discovery/routing"
	dutil "github.com/libp2p/go-libp2p/p2p/discovery/util"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	libp2pwebtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
)

// discoveryNotifee gets notified when we find a new peer via mDNS discovery
type discoveryNotifee struct {
	h   host.Host
	log log.Logger
}

// HandlePeerFound connects to peers discovered via mDNS
func (n *discoveryNotifee) HandlePeerFound(pi peer.AddrInfo) {
	if pi.ID == n.h.ID() {
		return // Skip connecting to self
	}

	ctx, cancel := context.WithTimeout(context.Background(), config.PEER_CONNECTION_TIMEOUT)
	defer cancel()

	err := n.h.Connect(ctx, pi)
	if err != nil {
		n.log.Debug("Failed connecting to peer " + pi.ID.String() + ": " + err.Error())
		return
	}
	n.log.Info("Connected to peer via mDNS: " + pi.ID.String())
}

// setupMDNS initializes the mDNS discovery service
func setupMDNS(h host.Host, log log.Logger) error {
	// Setup mDNS discovery service
	notifee := &discoveryNotifee{h: h, log: log}
	disc := libp2pmdns.NewMdnsService(h, config.DISCOVERY_TAG, notifee)
	disc.Start()
	return nil
}

// findPeersLoop continuously searches for peers using DHT discovery
func findPeersLoop(ctx context.Context, routingDiscovery *drouting.RoutingDiscovery, h host.Host, log log.Logger) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			// Find peers advertising under our discovery tag
			peerCh, err := routingDiscovery.FindPeers(ctx, config.DISCOVERY_TAG)
			if err != nil {
				log.Error("DHT FindPeers error", err)
				// Sleep before retry on error
				time.Sleep(5 * time.Second)
				continue
			}

			// Process all peers from this discovery round
			for peer := range peerCh {
				// Skip self
				if peer.ID == h.ID() {
					continue
				}

				// Use the host's native Connect method - libp2p will internally
				// prioritize direct connections when possible
				connectCtx, connectCancel := context.WithTimeout(ctx, config.PEER_CONNECTION_TIMEOUT)
				if err := h.Connect(connectCtx, peer); err != nil {
					log.Error("Failed to connect to discovered peer "+peer.ID.String(), err)
				} else {
					log.Info("Connected to discovered peer: " + peer.ID.String())
				}
				connectCancel()
			}

			// Wait before next discovery attempt
			time.Sleep(config.DHT_PEER_DISCOVERY_INTERVAL)
		}
	}
}

// setupDHT initializes the DHT, bootstraps it, and starts peer discovery
func setupDHT(ctx context.Context, h host.Host, log log.Logger) (*dht.IpfsDHT, error) {
	// Start a DHT, for use in peer discovery
	kademliaDHT, err := dht.New(ctx, h, dht.Mode(dht.ModeAuto))
	if err != nil {
		return nil, err
	}

	// Bootstrap the DHT
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return nil, err
	}

	// Connect to bootstrap peers
	peers, _ := peer.AddrInfosFromP2pAddrs(dht.DefaultBootstrapPeers...)
	for _, pInfo := range peers {
		connCtx, cancel := context.WithTimeout(ctx, config.PEER_CONNECTION_TIMEOUT)
		err := h.Connect(connCtx, pInfo)
		cancel()

		if err != nil {
			log.Error("Failed to connect to bootstrap peer "+pInfo.ID.String(), err)
			continue
		}
		log.Info("Connected to bootstrap peer " + pInfo.ID.String())
	}

	// Advertise ourselves using the discovery tag
	routingDiscovery := drouting.NewRoutingDiscovery(kademliaDHT)
	dutil.Advertise(ctx, routingDiscovery, config.DISCOVERY_TAG)

	// Start continuous peer discovery in a goroutine
	go findPeersLoop(ctx, routingDiscovery, h, log)

	return kademliaDHT, nil
}

// CreateLibp2pHost creates a new libp2p Host with default settings.
func CreateLibp2pHost(ctx context.Context, log log.Logger, opts ...libp2p.Option) (host.Host, error) {
	// We'll use a shared variable for the DHT instance
	// to avoid duplication between setupDHT and the routing constructor
	var kadDHT *dht.IpfsDHT
	var dhtErr error
	var dhtOnce sync.Once

	// Configure libp2p options
	options := []libp2p.Option{
		// Transport configuration with prioritized direct connection protocols
		libp2p.ChainOptions(
			// Prioritize transports that work well for direct connections
			libp2p.Transport(tcp.NewTCPTransport),     // TCP is most reliable for direct connections
			libp2p.Transport(libp2pquic.NewTransport), // QUIC works well through NATs
			libp2p.Transport(websocket.New),           // WebSockets can work through certain firewalls
			libp2p.Transport(libp2pwebrtc.New),        // WebRTC for browser compatibility
			libp2p.Transport(libp2pwebtransport.New),  // WebTransport for modern browser compatibility
		),

		// NAT traversal enhancements
		libp2p.EnableNATService(), // AutoNAT service
		libp2p.NATPortMap(),       // NAT port mapping

		// Enable hole punching
		libp2p.EnableHolePunching(),

		// Set up AutoRelay
		libp2p.EnableRelay(),

		// Configure AutoNAT
		libp2p.AutoNATServiceRateLimit(
			30, // 30 requests globally per minute
			3,  // 3 requests per peer per minute
			config.AUTONAT_REFRESH_INTERVAL,
		),

		// Create a routing constructor function that uses our DHT instance
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			// Initialize DHT only once
			dhtOnce.Do(func() {
				kadDHT, dhtErr = setupDHT(ctx, h, log)
			})
			return kadDHT, dhtErr
		}),

		// Enable relays and NAT services
		libp2p.EnableRelayService(), // Enable Relay Service if publicly reachable
		libp2p.EnableAutoNATv2(),    // Enable AutoNATv2
		libp2p.DisableMetrics(),     // Disable metrics collection for performance
	}

	// Add any user-provided options
	options = append(options, opts...)

	// Create a new libp2p Host
	h, err := libp2p.New(options...)
	if err != nil {
		log.Error("Failed to create libp2p host", err)
		return nil, err
	}

	// Setup mDNS discovery
	if err := setupMDNS(h, log); err != nil {
		log.Error("Failed to setup mDNS", err)
		// Continue anyway, some features may still work
	}

	// Log any DHT setup errors
	if dhtErr != nil {
		log.Error("Failed to setup DHT", dhtErr)
		// Continue anyway, some features may still work
	}

	// Log our listening addresses
	addresses := h.Addrs()
	log.Info(fmt.Sprintf("Created libp2p host with ID: %s", h.ID().String()))
	log.Info(fmt.Sprintf("Host is listening on %d addresses:", len(addresses)))
	for i, addr := range addresses {
		log.Info(fmt.Sprintf("  %d: %s", i+1, addr.String()))
	}

	return h, nil
}
