package config

import (
	"os"
	"time"

	"github.com/libp2p/go-libp2p/core/protocol"
)

// Protocol constants
const PROTOCOL_ID protocol.ID = "/drpc/1.0.0" // Keep the same protocol ID for now

// Connection constants
const CONNECTION_TIMEOUT = 30 * time.Second

// Debug variable from env
var DEBUG bool

func init() {
	DEBUG = os.Getenv("DEBUG") == "true" || os.Getenv("DEBUG") == "1"
}
