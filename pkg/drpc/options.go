package drpc

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

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
	// Fn to determine if the server should run in detached mode and return an error if did not run detached successfully
	detachedPredicateFunc func(*Server) error
}

func getDefaultConfig() cfg {
	l, _ := glog.New(glog.WithFileLogger("server.log"))
	return cfg{
		httpPort:               9090,
		httpHost:               "localhost",
		logger:                 l,
		dhtBootstrap:           true,
		forceCloseExistingPort: false,
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

// New option to set the predicator.
func WithDetachPredicateFunc(pred func(*Server) error) Option {
	return func(c *cfg) error {
		c.detachedPredicateFunc = pred
		return nil
	}
}

// WithDetachOnServerReadyPredicateFunc defines a predicate function that repeatedly queries the endpoint until
// it gets a valid (HTTP 200) response or times out.
func WithDetachOnServerReadyPredicateFunc() Option {
	fn := func(s *Server) error {
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			resp, err := http.Get(fmt.Sprintf("http://%s/p2pinfo", s.httpServer.Addr))
			if err != nil {
				if strings.Contains(err.Error(), "connection refused") {
					time.Sleep(500 * time.Millisecond)
					continue
				}
				return err
			}
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			return fmt.Errorf("unexpected status code: %v", resp.StatusCode)
		}
		return fmt.Errorf("detached server did not become ready within timeout")
	}

	return WithDetachPredicateFunc(fn)
}
