package drpc

import (
	"context"
	"sync"

	lp "github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	host "github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// bootstrap is an optional helper to connect to the given peers and bootstrap
// the Peer DHT (and Bitswap). This is a best-effort function. Errors are only
// logged and a warning is printed when less than half of the given peers
// could be contacted. It is fine to pass a list where some peers will not be
// reachable.
func bootstrap(ctx context.Context, d *dht.IpfsDHT, h host.Host, log glog.Logger) {
	connected := make(chan struct{})
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
			log.Printf("Connected to", pInfo.ID)
			connected <- struct{}{}
		}(pInfo)
	}

	go func() {
		wg.Wait()
		close(connected)
	}()

	i := 0
	for range connected {
		i++
	}
	if nPeers := len(peers); i < nPeers/2 {
		log.Printf("only connected to %d bootstrap peers out of %d", i, nPeers)
	}

	err := d.Bootstrap(ctx)
	if err != nil {
		log.Error("dht bootstrap failed - ", err)
		return
	}
}

// NewHost creates a new libp2p Host with default settings.
func NewHost(ctx context.Context, log glog.Logger, opts ...lp.Option) (host.Host, error) {
	var dhtInstance *dht.IpfsDHT

	// Configure libp2p options
	options := []lp.Option{
		lp.Defaults,
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
		lp.EnableAutoRelayWithStaticRelays([]peer.AddrInfo{}), // Enable AutoRelay with static relays (can be configured later)
	}
	options = append(options, opts...)

	// Create a new libp2p Host
	h, err := lp.New(options...)
	if err != nil {
		log.Error("Failed to create libp2p host: %s", err)
		return nil, err
	}

	// Bootstrap the DHT
	bootstrap(ctx, dhtInstance, h, log)

	return h, nil
}
