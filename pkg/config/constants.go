package config

import (
	"os"
	"strconv"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
)

// Protocol constants
const (
	version = "1.0.0"
	// DRPC_PROTOCOL_ID is the protocol identifier used for dRPC communications
	DRPC_PROTOCOL_ID protocol.ID = "/drpc/" + version
	// DRPC_WEB_STREAM_PROTOCOL_ID is used for web clients requiring a streaming bridge
	// to enable client-side and bidirectional streaming with ConnectRPC handlers.
	DRPC_WEB_STREAM_PROTOCOL_ID protocol.ID = "/drpc-webstream/" + version
)

// Connection constants
const (
	// CONNECTION_TIMEOUT is the maximum time to wait when establishing connections
	CONNECTION_TIMEOUT = 60 * time.Second
)

// Discovery constants
const (
	// DISCOVERY_TAG is the tag used for general peer discovery
	DISCOVERY_TAG = "drpc"

	// pubsub discovery topic
	DISCOVERY_PUBSUB_TOPIC = DISCOVERY_TAG + "._peer-discovery._p2p._pubsub"

	// DHT_PEER_DISCOVERY_INTERVAL is the interval between DHT peer discovery attempts
	DHT_PEER_DISCOVERY_INTERVAL = 60 * time.Second

	// PEER_CONNECTION_TIMEOUT is the timeout for connecting to a discovered peer
	PEER_CONNECTION_TIMEOUT = 60 * time.Second

	// AUTONAT_REFRESH_INTERVAL is how often to refresh NAT status
	AUTONAT_REFRESH_INTERVAL = 30 * time.Second
)

// DEBUG enables additional logging and debugging features
var DEBUG bool

func init() {
	// Parse DEBUG from environment, supporting various formats (true/false, 1/0, etc.)
	debug := os.Getenv("DEBUG")
	if debug != "" {
		DEBUG, _ = strconv.ParseBool(debug) // ParseBool handles "1", "t", "T", "true", "TRUE", etc.
	}
}
