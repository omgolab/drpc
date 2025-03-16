package core

import (
	"context"

	libp2p "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	libp2pquic "github.com/libp2p/go-libp2p/p2p/transport/quic"
	"github.com/libp2p/go-libp2p/p2p/transport/tcp"
	libp2pwebrtc "github.com/libp2p/go-libp2p/p2p/transport/webrtc"
	"github.com/libp2p/go-libp2p/p2p/transport/websocket"
	libp2pwebtransport "github.com/libp2p/go-libp2p/p2p/transport/webtransport"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// bootstrap is an optional helper to connect to the given peers and bootstrap
// the Peer DHT (and Bitswap). This is a best-effort function.
func bootstrap(ctx context.Context, d *dht.IpfsDHT, h host.Host, log glog.Logger) {
	if d == nil {
		return // Skip if DHT is not initialized
	}

	peers, _ := peer.AddrInfosFromP2pAddrs(dht.DefaultBootstrapPeers...)
	for _, pInfo := range peers {
		err := h.Connect(ctx, pInfo)
		if err != nil {
			log.Warn(err.Error())
			continue
		}
		log.Printf("Connected to %s", pInfo.ID)
	}

	if err := d.Bootstrap(ctx); err != nil {
		log.Error("DHT bootstrap failed - ", err)
		return
	}
}

// CreateLibp2pHost creates a new libp2p Host with default settings.
func CreateLibp2pHost(ctx context.Context, log glog.Logger, opts ...libp2p.Option) (host.Host, error) {
	var dhtInstance *dht.IpfsDHT

	// Configure libp2p options
	// Prioritize transports: WebTransport > WebRTC > WS > QUIC > TCP; also needs to be done in client
	options := []libp2p.Option{
		libp2p.ChainOptions(
			libp2p.Transport(libp2pwebtransport.New),
			libp2p.Transport(libp2pwebrtc.New),
			libp2p.Transport(websocket.New),
			libp2p.Transport(libp2pquic.NewTransport),
			libp2p.Transport(tcp.NewTCPTransport),
		),
		libp2p.EnableNATService(), // AutoNAT service
		libp2p.NATPortMap(),       // NAT port mapping
		// routing is needed to discover other peers in the network that may act as relay nodes
		libp2p.Routing(func(h host.Host) (routing.PeerRouting, error) {
			var err error
			dhtInstance, err = dht.New(ctx, h, dht.Mode(dht.ModeAuto))
			return dhtInstance, err
		}),
		libp2p.EnableHolePunching(), // Enable Hole punching
		libp2p.DefaultEnableRelay,   // Enable AutoRelay
		libp2p.EnableRelayService(), // Enable Relay Service if publicly reachable
	}
	options = append(options, opts...)

	// Create a new libp2p Host
	h, err := libp2p.New(options...)
	if err != nil {
		log.Error("Failed to create libp2p host: %s", err)
		return nil, err
	}

	// Bootstrap the DHT
	go bootstrap(ctx, dhtInstance, h, log)

	return h, nil
}
