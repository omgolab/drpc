package routes

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
)

// getP2PInfoHandler adapts the p2pInfoHandler to the middleware structure
func getP2PInfoHandler(h host.Host) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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
}
