package drpc

import (
	"errors"
	"fmt"

	// Import time package
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht" // Import DHT package
	"github.com/omgolab/drpc/pkg/detach"
	glog "github.com/omgolab/go-commons/pkg/log"
)

type cfg struct {
	httpPort               int
	httpHost               string
	logger                 glog.Logger
	libp2pOptions          []libp2p.Option
	dhtOptions             []dht.Option // New field for DHT options
	forceCloseExistingPort bool
	isDetachServer         bool
	detachOptions          []detach.DetachOption
}

func getDefaultConfig() cfg {
	l, _ := glog.New(glog.WithFileLogger("server.log"))
	return cfg{
		httpPort:               9090,
		httpHost:               "localhost",
		logger:                 l,
		dhtOptions:             nil, // Default to nil (core/host.go might apply defaults)
		forceCloseExistingPort: false,
		isDetachServer:         false,
		detachOptions:          nil, // Default to nil, detach package will apply defaults
	}
}

type ServerOption func(cfg *cfg) error

// WithLibP2POptions sets the libp2p options
func WithLibP2POptions(opts ...libp2p.Option) ServerOption {
	return func(cfg *cfg) error {
		cfg.libp2pOptions = opts
		return nil
	}
}

// WithDHTOptions sets the Kademlia DHT options
func WithDHTOptions(opts ...dht.Option) ServerOption {
	return func(cfg *cfg) error {
		cfg.dhtOptions = opts
		return nil
	}
}

// WithHTTPPort sets the HTTP gateway port
// default port is 90090
// pass -1 to disable HTTP server interface
func WithHTTPPort(port int) ServerOption {
	return func(cfg *cfg) error {
		if port < -1 || port > 65535 {
			return fmt.Errorf("invalid port number: %d", port)
		}
		cfg.httpPort = port
		return nil
	}
}

// WithHTTPHost sets the HTTP gateway host
// default is "localhost"
func WithHTTPHost(host string) ServerOption {
	return func(cfg *cfg) error {
		cfg.httpHost = host
		return nil
	}
}

// WithLogger sets the logger
func WithLogger(log glog.Logger) ServerOption {
	return func(cfg *cfg) error {
		if log == nil {
			return errors.New("invalid logger")
		}
		cfg.logger = log
		return nil
	}
}

// WithForceCloseExistingPort forces closing of any existing process on the configured port.
func WithForceCloseExistingPort(forceClose bool) ServerOption {
	return func(cfg *cfg) error {
		cfg.forceCloseExistingPort = forceClose
		return nil
	}
}

// WithDetachedServer configures whether the server should attempt to start in detached mode.
// Set to true when running within tests or embedded scenarios.
func WithDetachedServer() ServerOption {
	return func(cfg *cfg) error {
		cfg.isDetachServer = true
		return nil
	}
}

// WithDetachOptions sets the options for the detached process functionality.
// These options will be passed to the detach package when starting a detached server.
func WithDetachOptions(opts ...detach.DetachOption) ServerOption {
	return func(cfg *cfg) error {
		cfg.isDetachServer = true
		cfg.detachOptions = append(cfg.detachOptions, opts...)
		return nil
	}
}
