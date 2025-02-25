package core

import (
	"context"
	"sync"
	"sync/atomic"

	lp "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	host "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	webtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// bootstrap is an optional helper to connect to the given peers and bootstrap
// the Peer DHT (and Bitswap). This is a best-effort function.
func bootstrap(ctx context.Context, d *dht.IpfsDHT, h host.Host, log glog.Logger) {
	if d == nil {
		return // Skip if DHT is not initialized
	}

	var connectedCount int64
	peers, _ := peer.AddrInfosFromP2pAddrs(dht.DefaultBootstrapPeers...)
	var wg sync.WaitGroup

	for _, pInfo := range peers {
		wg.Add(1)
		go func(pInfo peer.AddrInfo) {
			defer wg.Done()
			err := h.Connect(ctx, pInfo)
			if err != nil {
				log.Warn(err.Error())
				return
			}
			log.Printf("Connected to %s", pInfo.ID)
			atomic.AddInt64(&connectedCount, 1)
		}(pInfo)
	}

	wg.Wait()
	if int(connectedCount) < len(peers)/2 {
		log.Printf("Only connected to %d bootstrap peers out of %d", connectedCount, len(peers))
	}

	if err := d.Bootstrap(ctx); err != nil {
		log.Error("DHT bootstrap failed - ", err)
		return
	}
}

// CreateLpHost creates a new libp2p Host with default settings.
func CreateLpHost(ctx context.Context, log glog.Logger, opts ...lp.Option) (host.Host, error) {
	var dhtInstance *dht.IpfsDHT

	// Configure libp2p options
	// Prioritize transports: WebTransport > WebRTC > WS > QUIC > TCP; also needs to be done in client
	options := []lp.Option{
		lp.ChainOptions(
			lp.Transport(webtransport.New),
			lp.Transport(libp2pwebrtc.New),
			lp.Transport(websocket.New),
			lp.Transport(libp2pquic.NewTransport),
			lp.Transport(tcp.NewTCPTransport),
		),
		lp.EnableNATService(), // AutoNAT service
		lp.NATPortMap(),       // NAT port mapping
		// routing is needed to discover other peers in the network that may act as relay nodes
		lp.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtInstance, err = dht.New(ctx, h, dht.Mode(dht.ModeAuto))
			return dhtInstance, err
		}),
		lp.EnableHolePunching(), // Enable Hole punching
		lp.DefaultEnableRelay,   // Enable AutoRelay
		lp.EnableRelayService(), // Enable Relay Service if publicly reachable
		// If the default bootstrap peers are insufficient for relay discovery add static relay addresses here.
		// lp.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{}), // Enable AutoRelay with static relays (can be configured later)
	}
	options = append(options, opts...)

	// Create a new libp2p Host
	h, err := lp.New(options...)
	if err != nil {
		log.Error("Failed to create libp2p host: %s", err)
		return nil, err
	}

	// Bootstrap the DHT
	go bootstrap(ctx, dhtInstance, h, log)

	return h, nil
}
