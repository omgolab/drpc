package gateway

import (
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// getGatewayHandler wraps a Connect RPC handler to support both direct and p2p gateway requests
func getGatewayHandler(logger glog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Printf("GetGatewayHandler - Raw URL Path: %s", r.URL.Path) // Log raw path

		addrs, servicePath, err := parseGatewayPath(r.URL.Path[2:], logger) // Remove the leading "/@"
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if len(addrs) == 0 {
			http.Error(w, "no valid addresses provided", http.StatusBadRequest)
			return
		}

		// First address must contain peer ID
		peerIDStr := extractPeerID(addrs[0])
		if peerIDStr == "" {
			http.Error(w, "first address must contain peer ID", http.StatusBadRequest)
			return
		}

		targetAddr, err := resolveMultiaddrs(peerIDStr, addrs)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		logger.Printf("GetGatewayHandler - targetAddr: %s", targetAddr)
		logger.Printf("GetGatewayHandler - servicePath: %s", servicePath)

		if err := forwardRequest(w, r, targetAddr, servicePath, logger); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func SetupRoutes(mux *http.ServeMux, handler http.Handler, logger glog.Logger, p2pHost host.Host) {
	// configure the gateway handler on the given ServeMux.  Only handle paths starting with /@/*
	mux.Handle("/@/*", getGatewayHandler(logger))

	// configure the P2P info handler on the given ServeMux.
	mux.HandleFunc("/p2pinfo", getP2PInfoHandler(p2pHost))

	// let the remaining paths be handled by the given handler
	mux.Handle("/", handler)
}
