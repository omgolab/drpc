package drpc

import (
	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// clientCfg holds the client configuration
type clientCfg struct {
	logger        glog.Logger // Change from pointer to interface
	connectOpts   []connect.ClientOption
	libp2pOptions []libp2p.Option
	dhtOptions    []dht.Option
}

// ClientOption configures a Client.
type ClientOption func(*clientCfg) error

// WithClientLogger sets the logger for the client.
func WithClientLogger(logger glog.Logger) ClientOption {
	return func(c *clientCfg) error {
		c.logger = logger
		return nil
	}
}

// WithConnectOptions adds Connect RPC client options.
func WithConnectOptions(opts ...connect.ClientOption) ClientOption {
	return func(c *clientCfg) error {
		if c.connectOpts == nil {
			c.connectOpts = make([]connect.ClientOption, 0)
		}
		c.connectOpts = append(c.connectOpts, opts...)
		return nil
	}
}

// WithClientLibp2pOptions sets the libp2p options for the client's host.
func WithClientLibp2pOptions(opts ...libp2p.Option) ClientOption {
	return func(c *clientCfg) error {
		if c.libp2pOptions == nil {
			c.libp2pOptions = make([]libp2p.Option, 0)
		}
		c.libp2pOptions = append(c.libp2pOptions, opts...)
		return nil
	}
}

// WithClientDHTOptions sets the Kademlia DHT options for the client's host.
func WithClientDHTOptions(opts ...dht.Option) ClientOption {
	return func(c *clientCfg) error {
		if c.dhtOptions == nil {
			c.dhtOptions = make([]dht.Option, 0)
		}
		c.dhtOptions = append(c.dhtOptions, opts...)
		return nil
	}
}

func (c *clientCfg) applyOptions(opts ...ClientOption) error {
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return err
		}
	}
	return nil
}
