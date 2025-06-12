package host

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestWithBroadcastInterval(t *testing.T) {
	cfg := &hostCfg{}

	// Test setting a custom broadcast interval
	interval := 15 * time.Second
	option := WithBroadcastInterval(interval)

	err := option(cfg)
	assert.NoError(t, err)
	assert.Equal(t, interval, cfg.broadcastInterval)
}

func TestDefaultBroadcastInterval(t *testing.T) {
	cfg := &hostCfg{}

	// By default, broadcastInterval should be zero
	assert.Equal(t, time.Duration(0), cfg.broadcastInterval)
}

func TestMultipleOptions(t *testing.T) {
	cfg := &hostCfg{}

	// Test applying multiple options
	interval := 45 * time.Second
	options := []HostOption{
		WithBroadcastInterval(interval),
		WithPubsubDiscovery(false),
	}

	for _, opt := range options {
		err := opt(cfg)
		assert.NoError(t, err)
	}

	assert.Equal(t, interval, cfg.broadcastInterval)
	assert.False(t, cfg.disablePubsubDiscovery)
}
