package host

import (
	"time"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// hostCfg holds the host configuration
type hostCfg struct {
	logger        glog.Logger
	libp2pOptions []libp2p.Option
	dhtOptions    []dht.Option
	isClientMode  bool
	// relayManager removed - libp2p's built-in AutoRelay handles this automatically
	disablePubsubDiscovery bool
	broadcastInterval      time.Duration // Configurable broadcast interval for peer discovery
}

// HostOption configures a Host.
type HostOption func(*hostCfg) error

// WithHostLogger sets the logger for the host.
func WithHostLogger(logger glog.Logger) HostOption {
	return func(c *hostCfg) error {
		c.logger = logger
		return nil
	}
}

// WithHostLibp2pOptions adds libp2p options to the host.
func WithHostLibp2pOptions(opts ...libp2p.Option) HostOption {
	return func(c *hostCfg) error {
		if c.libp2pOptions == nil {
			c.libp2pOptions = make([]libp2p.Option, 0)
		}
		c.libp2pOptions = append(c.libp2pOptions, opts...)
		return nil
	}
}

// WithHostDHTOptions adds DHT options to the host.
func WithHostDHTOptions(opts ...dht.Option) HostOption {
	return func(c *hostCfg) error {
		if c.dhtOptions == nil {
			c.dhtOptions = make([]dht.Option, 0)
		}
		c.dhtOptions = append(c.dhtOptions, opts...)
		return nil
	}
}

// WithHostAsClientMode marks the host to operate in client mode (no server DHT bootstrap).
func WithHostAsClientMode() HostOption {
	return func(c *hostCfg) error {
		// client mode implies disabling server ModeAuto (no bootstrap)
		c.dhtOptions = append(c.dhtOptions, dht.Mode(dht.ModeClient))
		c.isClientMode = true
		return nil
	}
}

// WithPubsubDiscovery enables pubsub discovery for the host.
func WithPubsubDiscovery(isDisable bool) HostOption {
	return func(c *hostCfg) error {
		c.disablePubsubDiscovery = isDisable
		return nil
	}
}

// WithBroadcastInterval sets the interval for broadcasting peer presence.
// If not set, defaults to 30 seconds.
func WithBroadcastInterval(interval time.Duration) HostOption {
	return func(c *hostCfg) error {
		c.broadcastInterval = interval
		return nil
	}
}
