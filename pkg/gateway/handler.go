package gateway

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// SetupHandler creates a new http.Handler with gateway functionality
func SetupHandler(baseHandler http.Handler, logger glog.Logger, p2pHost host.Host) http.Handler {
	mux := http.NewServeMux()

	// Add gateway handler for /@/ path pattern
	mux.HandleFunc("/@/", func(w http.ResponseWriter, r *http.Request) {
		// Parse addresses and service path from the URL
		peerAddrs, servicePath, err := ParseAddresses(r.URL.Path)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to parse addresses: %v", err), http.StatusBadRequest)
			return
		}

		// Get the first peer's addresses
		var selectedAddrs []string
		for _, addrs := range peerAddrs {
			for _, addr := range addrs {
				selectedAddrs = append(selectedAddrs, addr.String())
			}
			break // only use first peer's addresses
		}

		// fix this:
		// Extract peer ID from the address
		peerIDStr := extractPeerID(addrs[0])
		if peerIDStr == "" {
			http.Error(w, "first address must contain peer ID", http.StatusBadRequest)
			return
		}

		// Parse the peer ID
		peerID, err := peer.Decode(peerIDStr)
		if err != nil {
			http.Error(w, "invalid peer ID: "+err.Error(), http.StatusBadRequest)
			return
		}

		logger.Printf("P2PGatewayHandler - PeerID: %s", peerID.String())
		logger.Printf("P2PGatewayHandler - ServicePath: %s", servicePath)

		// Create a new stream to the target peer
		stream, err := p2pHost.NewStream(r.Context(), peerID, protoID)
		if err != nil {
			logger.Error("Failed to create stream to peer", err)
			http.Error(w, "failed to connect to peer: "+err.Error(), http.StatusInternalServerError)
			return
		}
		defer stream.Close()

		// Forward the request through the libp2p stream
		if err := forwardRequestViaStream(w, r, stream, servicePath, logger); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	})

	// Add info endpoint
	mux.HandleFunc("/p2pinfo", func(w http.ResponseWriter, r *http.Request) {
		p2pInfoHandler(w, r, p2pHost, logger)
	})

	// For all other paths, use the base handler
	mux.Handle("/", baseHandler)

	return mux
}

// p2pInfoHandler returns information about the p2p host
func p2pInfoHandler(w http.ResponseWriter, r *http.Request, h host.Host, logger glog.Logger) {
	// Enable CORS for development
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type")

	if r.Method == "OPTIONS" {
		w.WriteHeader(http.StatusOK)
		return
	}

	info := struct {
		ID    string   `json:"ID"`
		Addrs []string `json:"Addrs"`
		Port  string   `json:"Port"`
	}{
		ID:    h.ID().String(),
		Addrs: make([]string, 0, len(h.Addrs())),
	}

	// Add addresses with peer ID
	for _, addr := range h.Addrs() {
		info.Addrs = append(info.Addrs, addr.String()+"/p2p/"+h.ID().String())
	}

	// Extract the HTTP port
	for _, addr := range h.Addrs() {
		if strings.HasPrefix(addr.String(), "http") {
			_, port, err := net.SplitHostPort(strings.TrimPrefix(addr.String(), "http://"))
			if err == nil {
				info.Port = port
			}
			break // Assuming only one HTTP address
		}
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Printf("Peer info: %+v\n", info) // Debug print
	if err := json.NewEncoder(w).Encode(&info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
