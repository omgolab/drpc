package server

import (
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/omgolab/drpc/pkg/detach"
	"github.com/omgolab/drpc/pkg/gateway"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// Config holds all server configuration options
type Config struct {
	httpPort               int
	httpHost               string
	logger                 glog.Logger
	libp2pOptions          []libp2p.Option
	dhtOptions             []dht.Option
	forceCloseExistingPort bool
	isDetachServer         bool
	detachOptions          []detach.DetachOption
	corsConfig             *gateway.CORSConfig
}

// GetDefaultConfig returns a default server configuration
func GetDefaultConfig() Config {
	l, _ := glog.New(glog.WithFileLogger("server.log"))
	return Config{
		httpHost:               "localhost",
		logger:                 l,
		dhtOptions:             nil, // Default to nil (core/host.go might apply defaults)
		forceCloseExistingPort: false,
		isDetachServer:         false,
		detachOptions:          nil, // Default to nil, detach package will apply defaults
	}
}

// ServerOption configures a Server
type ServerOption func(cfg *Config) error

// WithLibP2POptions sets the libp2p options
func WithLibP2POptions(opts ...libp2p.Option) ServerOption {
	return func(cfg *Config) error {
		cfg.libp2pOptions = opts
		return nil
	}
}

// WithDHTOptions sets the Kademlia DHT options
func WithDHTOptions(opts ...dht.Option) ServerOption {
	return func(cfg *Config) error {
		cfg.dhtOptions = opts
		return nil
	}
}

// WithHTTPPort sets the HTTP port for the server
func WithHTTPPort(port int) ServerOption {
	return func(cfg *Config) error {
		if port < -1 || port > 65535 {
			return errors.New("invalid HTTP port; must be -1 (disabled), 0 (auto), or 1-65535")
		}
		cfg.httpPort = port
		return nil
	}
}

// WithHTTPHost sets the HTTP host for the server
func WithHTTPHost(host string) ServerOption {
	return func(cfg *Config) error {
		if host == "" {
			return errors.New("HTTP host cannot be empty")
		}
		cfg.httpHost = host
		return nil
	}
}

// WithLogger sets the logger for the server
func WithLogger(logger glog.Logger) ServerOption {
	return func(cfg *Config) error {
		if logger == nil {
			return errors.New("logger cannot be nil")
		}
		cfg.logger = logger
		return nil
	}
}

// WithForceCloseExistingPort forces closing existing ports if they're in use
func WithForceCloseExistingPort(force bool) ServerOption {
	return func(cfg *Config) error {
		cfg.forceCloseExistingPort = force
		return nil
	}
}

// WithDetachServer enables detached server mode with optional detach options
func WithDetachServer(detachOpts ...detach.DetachOption) ServerOption {
	return func(cfg *Config) error {
		cfg.isDetachServer = true
		cfg.detachOptions = detachOpts
		return nil
	}
}

// WithDisableHTTP disables the HTTP server interface
func WithDisableHTTP() ServerOption {
	return func(cfg *Config) error {
		cfg.httpPort = -1
		return nil
	}
}

// WithTLS enables TLS/SSL support
func WithTLS(certFile, keyFile string) ServerOption {
	return func(cfg *Config) error {
		if certFile == "" || keyFile == "" {
			return fmt.Errorf("TLS requires both cert file and key file")
		}
		// Future: Add TLS configuration
		return nil
	}
}

// WithCORSHeaders enables CORS headers with configurable options
func WithCORSHeaders(origins, methods, headers, exposedHeaders []string) ServerOption {
	return func(cfg *Config) error {
		// Default values if not provided
		if len(origins) == 0 {
			origins = []string{"*"}
		}
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "OPTIONS"}
		}
		if len(headers) == 0 {
			headers = []string{"Content-Type", "Accept", "Authorization", "Connect-Accept-Encoding", "Connect-Content-Encoding", "Connect-Protocol-Version", "Connect-Timeout-Ms"}
		}
		if len(exposedHeaders) == 0 {
			exposedHeaders = []string{"Content-Type", "Connect-Content-Encoding"}
		}

		cfg.corsConfig = &gateway.CORSConfig{
			AllowedOrigins: origins,
			AllowedMethods: methods,
			AllowedHeaders: headers,
			ExposedHeaders: exposedHeaders,
		}
		return nil
	}
}

// WithDefaultCORSHeaders enables CORS headers with sensible defaults for development
func WithDefaultCORSHeaders() ServerOption {
	return WithCORSHeaders(nil, nil, nil, nil)
}
