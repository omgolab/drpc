package drpc

import glog "github.com/omgolab/go-commons/pkg/log"

// clientCfg represents a dRPC client
type clientCfg struct {
	logger glog.Logger
	// Add other client configuration options as needed
}

// ClientOption is a function that configures a Client
type ClientOption func(*clientCfg) error

// WithLogger sets the logger for the client
func WithClientLogger(logger glog.Logger) ClientOption {
	return func(c *clientCfg) error {
		c.logger = logger
		return nil
	}
}

// Apply applies the client options method
func (c *clientCfg) applyOptions(opts ...ClientOption) error {
	for _, opt := range opts {
		if err := opt(c); err != nil {
			return err
		}
	}
	if c.logger == nil {
		var err error
		c.logger, err = glog.New()
		if err != nil {
			return err
		}
	}
	return nil
}
