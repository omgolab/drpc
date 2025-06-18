package host

//go:generate sh -c "cd ../proto/; buf generate"

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	connmgr "github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/omgolab/drpc/pkg/config"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// CreateLibp2pHost creates a new libp2p Host with default settings.
func CreateLibp2pHost(ctx context.Context, opts ...HostOption) (host.Host, error) {
	// apply HostOption to build config
	cfg := &hostCfg{}
	for _, o := range opts {
		if err := o(cfg); err != nil {
			return nil, err
		}
	}
	log := cfg.logger
	libp2pOpts := cfg.libp2pOptions
	dhtOpts := cfg.dhtOptions

	// We'll use a shared variable for the DHT instance
	// to avoid duplication between setupDHT and the routing constructor
	var kadDHT *dht.IpfsDHT
	var dhtErr error
	var dhtOnce sync.Once

	// fix for nil options
	if dhtOpts == nil {
		dhtOpts = []dht.Option{}
	}
	if libp2pOpts == nil {
		libp2pOpts = []libp2p.Option{}
	}

	// Configure libp2p options
	options := []libp2p.Option{
		// Listen on default addresses with WebSocket explicitly listed first for browser compatibility
		libp2p.ListenAddrStrings("/ip4/0.0.0.0/tcp/0/ws", "/ip4/0.0.0.0/tcp/0"),
		libp2p.ShareTCPListener(),

		// Connection management to prevent memory leaks from connection accumulation
		libp2p.ConnectionManager(func() *connmgr.BasicConnMgr {
			cm, _ := connmgr.NewConnManager(
				100,                                  // low watermark - start closing connections when we reach this many
				400,                                  // high watermark - maximum connections before more aggressive pruning
				connmgr.WithGracePeriod(time.Minute), // give connections time to stabilize
			)
			return cm
		}()),

		// Use ResourceManager and other defaults (but not FallbackDefaults to avoid connection manager conflict)
		libp2p.DefaultMuxers,
		libp2p.DefaultTransports,
		libp2p.DefaultSecurity,

		// NAT traversal enhancements
		libp2p.EnableNATService(), // AutoNAT service
		libp2p.NATPortMap(),       // NAT port mapping

		// Enable hole punching
		libp2p.EnableHolePunching(),

		// Set up AutoRelay
		libp2p.EnableRelay(),

		// Configure AutoNAT
		libp2p.AutoNATServiceRateLimit(
			10,          // global rate limit
			3,           // per peer rate limit
			time.Minute, // interval
		),

		// Set up routing with DHT
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			// Use sync.Once to ensure DHT is only initialized once
			dhtOnce.Do(func() {
				kadDHT, dhtErr = setupDHT(ctx, h, cfg, dhtOpts...)
			})
			return kadDHT, dhtErr
		}),
	}

	// Add user-provided libp2p options
	options = append(options, libp2pOpts...)

	// Create host
	h, err := libp2p.New(options...)
	if err != nil {
		return nil, err
	}

	log.Info("libp2p host created", glog.LogFields{
		"peerID":    h.ID().String(),
		"addrs":     h.Addrs(),
		"protocols": h.Mux().Protocols(),
	})

	// Set up discovery services
	if err := setupMDNS(h, cfg); err != nil {
		log.Error("Failed to set up mDNS discovery", err)
		// Don't return error - mDNS is optional
	}

	// Set up pubsub discovery if not disabled
	if !cfg.disablePubsubDiscovery {
		if err := setupPubsubDiscovery(ctx, h, cfg); err != nil {
			log.Error("Failed to set up pubsub discovery", err)
			// Don't return error - pubsub is optional
		}
	}

	return h, nil
}

// setupPubsubDiscovery sets up pubsub-based peer discovery
func setupPubsubDiscovery(ctx context.Context, h host.Host, cfg *hostCfg) error {
	// Create a new PubSub service using GossipSub
	ps, err := pubsub.NewGossipSub(ctx, h)
	if err != nil {
		return err
	}

	// Join the discovery topic
	topic, err := ps.Join(config.DISCOVERY_PUBSUB_TOPIC)
	if err != nil {
		return err
	}

	// Subscribe to the topic
	subscription, err := topic.Subscribe()
	if err != nil {
		return err
	}

	cfg.logger.Info("Joined pubsub discovery topic", glog.LogFields{"topic": config.DISCOVERY_PUBSUB_TOPIC})

	// Start listening for messages (discovery announcements from other peers)
	go handlePubsubMessages(ctx, subscription, h, cfg)

	// Start broadcasting our presence periodically
	go broadcastPeerPresence(ctx, h, topic, subscription, cfg)

	return nil
}

// handlePubsubMessages processes incoming pubsub discovery messages
func handlePubsubMessages(ctx context.Context, subscription *pubsub.Subscription, h host.Host, cfg *hostCfg) {
	for {
		msg, err := subscription.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				cfg.logger.Info("Stopping pubsub message handling due to context cancellation")
				return
			}
			cfg.logger.Error("Error reading pubsub message", err)
			continue
		}

		// Skip messages from ourselves
		if msg.ReceivedFrom == h.ID() {
			continue
		}

		// Try to connect to the peer who sent the message
		peerInfo := peer.AddrInfo{ID: msg.ReceivedFrom}

		// Create a context for this connection attempt with a timeout
		connCtx, cancel := context.WithTimeout(ctx, 15*time.Second)

		go func() {
			defer cancel()

			if err := h.Connect(connCtx, peerInfo); err != nil {
				cfg.logger.Debug("Failed to connect to peer from pubsub", glog.LogFields{
					"peer":  peerInfo.ID.String(),
					"error": err.Error(),
				})
			} else {
				cfg.logger.Debug("Connected to peer from pubsub", glog.LogFields{
					"peer": peerInfo.ID.String(),
				})
			}
		}()
	}
}
