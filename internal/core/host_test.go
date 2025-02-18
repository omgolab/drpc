package core

import (
	"context"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// TestCreateLpHostSuccess verifies CreateLpHost creates a valid host.
func TestCreateLpHostSuccess(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	h, err := CreateLpHost(ctx, logger)
	if err != nil {
		t.Fatalf("CreateLpHost error: %v", err)
	}
	if h == nil {
		t.Fatal("CreateLpHost returned nil host")
	}
	// Ensure proper shutdown.
	if err := h.Close(); err != nil {
		t.Errorf("Host close failed: %v", err)
	}
}

// TestCreateLpHostMultiple ensures that multiple hosts can be created concurrently.
func TestCreateLpHostMultiple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	logger, _ := glog.New()
	var hosts []host.Host
	for i := 0; i < 3; i++ {
		h, err := CreateLpHost(ctx, logger)
		if err != nil {
			t.Fatalf("Iteration %d: CreateLpHost error: %v", i, err)
		}
		hosts = append(hosts, h)
	}
	for i, h := range hosts {
		if h == nil {
			t.Errorf("Host %d is nil", i)
		}
		if err := h.Close(); err != nil {
			t.Errorf("Closing host %d failed: %v", i, err)
		}
	}
}

// TestHostShutdown verifies that a host shuts down properly.
func TestHostShutdown(t *testing.T) {
	ctx := context.Background()
	logger, _ := glog.New()

	h, err := CreateLpHost(ctx, logger)
	if err != nil {
		t.Fatalf("CreateLpHost error: %v", err)
	}
	if err := h.Close(); err != nil {
		t.Errorf("Host shutdown error: %v", err)
	}
}
