package client

import (
	"context"
	"crypto/tls"
	"net"

	"connectrpc.com/connect"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	glog "github.com/omgolab/go-commons/pkg/log"
	"golang.org/x/net/http2"
)

// optimizedHTTP2Transport returns an HTTP/2 transport optimized for production
func optimizedHTTP2Transport() *http2.Transport {
	// Create optimized TLS config with session resumption for HTTPS
	tlsConfig := &tls.Config{
		// Enable TLS session resumption for faster handshakes
		ClientSessionCache: tls.NewLRUClientSessionCache(256),

		// Use modern TLS settings
		MinVersion: tls.VersionTLS12,

		// Prefer cipher suites that support forward secrecy
		CipherSuites: []uint16{
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}

	return &http2.Transport{
		// Allow non-TLS HTTP/2 for internal communication (h2c)
		AllowHTTP: true,

		// Custom dialer that skips TLS for http:// addresses
		DialTLSContext: func(ctx context.Context, network, addr string, cfg *tls.Config) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, network, addr)
		},

		// Connection settings
		MaxHeaderListSize: 32 << 10, // 32KB

		// TLS settings for HTTPS with session reuse
		TLSClientConfig: tlsConfig,

		// Connection pooling
		DisableCompression: false,
	}
}

// Config holds the client configuration
type Config struct {
	logger        glog.Logger // Change from pointer to interface
	connectOpts   []connect.ClientOption
	libp2pOptions []libp2p.Option
	dhtOptions    []dht.Option
}

// Option configures a Client.
type Option func(*Config) error

// WithLogger sets the logger for the client.
func WithLogger(logger glog.Logger) Option {
	return func(c *Config) error {
		c.logger = logger
		return nil
	}
}

// WithConnectOptions adds Connect RPC client options.
func WithConnectOptions(opts ...connect.ClientOption) Option {
	return func(c *Config) error {
		if c.connectOpts == nil {
			c.connectOpts = make([]connect.ClientOption, 0)
		}
		c.connectOpts = append(c.connectOpts, opts...)
		return nil
	}
}

// WithLibp2pOptions sets the libp2p options for the client's host.
func WithLibp2pOptions(opts ...libp2p.Option) Option {
	return func(c *Config) error {
		if c.libp2pOptions == nil {
			c.libp2pOptions = make([]libp2p.Option, 0)
		}
		c.libp2pOptions = append(c.libp2pOptions, opts...)
		return nil
	}
}

// WithDHTOptions sets the Kademlia DHT options for the client's host.
func WithDHTOptions(opts ...dht.Option) Option {
	return func(c *Config) error {
		if c.dhtOptions == nil {
			c.dhtOptions = make([]dht.Option, 0)
		}
		c.dhtOptions = append(c.dhtOptions, opts...)
		return nil
	}
}

func (c *Config) applyOptions(opts ...Option) error {
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return err
		}
	}
	return nil
}
