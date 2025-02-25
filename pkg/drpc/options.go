package drpc

import (
	"errors"
	"fmt"

	"github.com/libp2p/go-libp2p"
	glog "github.com/omgolab/go-commons/pkg/log"
)

type cfg struct {
	httpPort               int
	httpHost               string
	logger                 glog.Logger
	libp2pOptions          []libp2p.Option
	dhtBootstrap           bool
	forceCloseExistingPort bool
	isDetachedServer       bool
}

func getDefaultConfig() cfg {
	l, _ := glog.New(glog.WithFileLogger("server.log"))
	return cfg{
		httpPort:               9090,
		httpHost:               "localhost",
		logger:                 l,
		dhtBootstrap:           true,
		forceCloseExistingPort: false,
		isDetachedServer:       false,
	}
}

type Option func(cfg *cfg) error

// WithLibP2POptions sets the libp2p options
func WithLibP2POptions(opts ...libp2p.Option) Option {
	return func(cfg *cfg) error {
		cfg.libp2pOptions = opts
		return nil
	}
}

// WithHTTPPort sets the HTTP gateway port
// default port is 90090
// pass -1 to disable HTTP server interface
func WithHTTPPort(port int) Option {
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
func WithHTTPHost(host string) Option {
	return func(cfg *cfg) error {
		cfg.httpHost = host
		return nil
	}
}

// WithLogger sets the logger
func WithLogger(log glog.Logger) Option {
	return func(cfg *cfg) error {
		if log == nil {
			return errors.New("invalid logger")
		}
		cfg.logger = log
		return nil
	}
}

// WithForceCloseExistingPort forces closing of any existing process on the configured port.
func WithForceCloseExistingPort(forceClose bool) Option {
	return func(cfg *cfg) error {
		cfg.forceCloseExistingPort = forceClose
		return nil
	}
}

// WithNoBootstrap disables bootstrapping
func WithNoBootstrap(isEnabled bool) Option {
	return func(cfg *cfg) error {
		cfg.dhtBootstrap = isEnabled
		return nil
	}
}

// New option to set the isDetachedServer flag
func WithDetachedServer() Option {
	return func(cfg *cfg) error {
		cfg.isDetachedServer = true
		return nil
	}
}
