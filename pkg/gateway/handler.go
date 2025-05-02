package gateway

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// SetupHandler creates a new http.Handler with gateway functionality
func SetupHandler(baseHandler http.Handler, logger glog.Logger, p2pHost host.Host) http.Handler {
	mux := http.NewServeMux()

	// Add gateway handler for GatewayPrefix path pattern
	mux.HandleFunc(GatewayPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		// Use the comprehensive function that handles everything in one place
		ForwardHTTPRequest(w, r, p2pHost, logger)
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
	if err := json.NewEncoder(w).Encode(&info); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
