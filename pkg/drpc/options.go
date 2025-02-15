package drpc

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/libp2p/go-libp2p"
	glog "github.com/omgolab/go-commons/pkg/log"
)

type cfg struct {
	httpPort      string
	httpHost      string
	logger        glog.Logger
	enableHTTP    bool
	Libp2pOptions []libp2p.Option
	Bootstrap     bool
}

func getDefaultConfig() cfg {
	l, _ := glog.New()
	return cfg{
		httpPort:   "9090",
		httpHost:   "localhost",
		logger:     l,
		enableHTTP: true,
		Bootstrap:  true,
	}
}

type Option func(cfg *cfg) error

// WithLibP2POptions sets the libp2p options
func WithLibP2POptions(opts ...libp2p.Option) Option {
	return func(cfg *cfg) error {
		cfg.Libp2pOptions = opts
		return nil
	}
}

// WithHTTPPort sets the HTTP gateway port
// default port is 90090
func WithHTTPPort(port int) Option {
	return func(cfg *cfg) error {
		if port < 0 || port > 65535 {
			return fmt.Errorf("invalid port number: %d", port)
		}
		cfg.httpPort = strconv.Itoa(port)
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

// WithHTTPEnabled enables or disables the HTTP gateway
// default is true
func WithHTTPEnabled(enabled bool) Option {
	return func(cfg *cfg) error {
		cfg.enableHTTP = enabled
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

// WithNoBootstrap disables bootstrapping
func WithNoBootstrap(isEnabled bool) Option {
	return func(cfg *cfg) error {
		cfg.Bootstrap = isEnabled
		return nil
	}
}
