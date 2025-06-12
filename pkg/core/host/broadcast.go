package host

import (
	"context"
	"fmt"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/omgolab/drpc/pkg/core/pool"
	pb "github.com/omgolab/drpc/pkg/core/proto"
	"google.golang.org/protobuf/proto"
)

// Protobuf message pool for peer data messages
var peerDataPool = pool.NewProtobufMessagePool(func() any {
	return &pb.Peer{}
})

// broadcastPeerPresence periodically broadcasts our presence via pubsub
func broadcastPeerPresence(ctx context.Context, h host.Host, topic *pubsub.Topic, subscription *pubsub.Subscription, cfg *hostCfg) {
	defer subscription.Cancel()

	// Use configured broadcast interval, default to 30 seconds if not set
	interval := cfg.broadcastInterval
	if interval == 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Helper function to create protobuf-compatible peer data using pooled messages
	createPeerData := func() (*pb.Peer, error) {
		// Get a pooled peer message
		peerData := peerDataPool.Get().(*pb.Peer)

		// Reset the message for reuse
		peerData.Reset()

		// Get our public key
		pubKey, err := h.ID().ExtractPublicKey()
		if err != nil {
			peerDataPool.Put(peerData) // Return to pool on error
			return nil, fmt.Errorf("failed to extract public key from peer ID: %w", err)
		}

		// Marshal public key to bytes (libp2p protobuf format)
		pubKeyBytes, err := pubKey.Raw()
		if err != nil {
			peerDataPool.Put(peerData) // Return to pool on error
			return nil, fmt.Errorf("failed to get raw public key: %w", err)
		}

		// Get our addresses as bytes
		peerData.Addrs = peerData.Addrs[:0] // Reset slice but keep capacity
		for _, addr := range h.Addrs() {
			peerData.Addrs = append(peerData.Addrs, addr.Bytes())
		}

		peerData.PublicKey = pubKeyBytes
		return peerData, nil
	}

	// Create initial peer data
	peerData, err := createPeerData()
	if err != nil {
		cfg.logger.Error("Failed to create peer data", err)
		return
	}

	// Marshal to protobuf bytes
	msgBytes, err := proto.Marshal(peerData)
	if err != nil {
		cfg.logger.Error("Failed to marshal peer data", err)
		peerDataPool.Put(peerData) // Return to pool on error
		return
	}

	// Check if there are subscribers before broadcasting
	if len(topic.ListPeers()) == 0 {
		// cfg.logger.Debug("Skipping initial broadcast: no peers subscribed to discovery topic")
	} else {
		// Send initial discovery message
		if err := topic.Publish(ctx, msgBytes); err != nil {
			cfg.logger.Error("Failed to publish initial discovery message", err)
		} else {
			cfg.logger.Info(fmt.Sprintf("Published initial discovery message (peer ID: %s, addrs: %d)",
				h.ID().String(), len(peerData.Addrs)))
		}
	}

	// Return peer data to pool after use
	peerDataPool.Put(peerData)

	// Start periodic broadcasting
	for {
		select {
		case <-ctx.Done():
			cfg.logger.Info("Stopping pubsub discovery broadcasting due to context cancellation")
			return
		case <-ticker.C:
			// Check if there are subscribers
			peers := topic.ListPeers()
			if len(peers) == 0 {
				// cfg.logger.Debug("Skipping broadcast: no peers subscribed to discovery topic")
				continue
			}

			// Create updated peer data (addresses might have changed)
			peerData, err := createPeerData()
			if err != nil {
				cfg.logger.Error("Failed to create peer data", err)
				continue
			}

			// Marshal updated peer data
			msgBytes, err := proto.Marshal(peerData)
			if err != nil {
				cfg.logger.Error("Failed to marshal peer data", err)
				peerDataPool.Put(peerData) // Return to pool on error
				continue
			}

			// Publish discovery message
			if err := topic.Publish(ctx, msgBytes); err != nil {
				cfg.logger.Error("Failed to publish discovery message", err)
			} else {
				// cfg.logger.Debug(fmt.Sprintf("Published discovery message (peer ID: %s, addrs: %d, subscribers: %d)",
				//	h.ID().String(), len(peerData.Addrs), len(peers)))
			}

			// Return peer data to pool after use
			peerDataPool.Put(peerData)
		}
	}
}
