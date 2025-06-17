package gateway

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// CORSConfig holds CORS configuration (same as server package)
type CORSConfig struct {
	AllowedOrigins []string
	AllowedMethods []string
	AllowedHeaders []string
	ExposedHeaders []string
}

// SetupHandler creates a new http.Handler with gateway functionality
func SetupHandler(baseHandler http.Handler, logger glog.Logger, p2pHost host.Host, corsConfig *CORSConfig) http.Handler {
	mux := http.NewServeMux()

	// Add gateway handler for GatewayPrefix path pattern
	mux.HandleFunc(GatewayPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		// Only add CORS headers if this is a preflight request or if CORS is enabled
		if corsConfig != nil {
			// Handle preflight requests
			if r.Method == http.MethodOptions {
				setCORSHeaders(w, corsConfig)
				w.WriteHeader(http.StatusOK)
				return
			}
			// For actual requests, only set CORS headers, don't wrap in middleware
			setCORSHeaders(w, corsConfig)
		}

		// Use the comprehensive function that handles everything in one place
		ForwardHTTPRequest(w, r, p2pHost, logger)
	})

	// Add info endpoint
	mux.HandleFunc("/p2pinfo", func(w http.ResponseWriter, r *http.Request) {
		if corsConfig != nil && r.Method == http.MethodOptions {
			setCORSHeaders(w, corsConfig)
			w.WriteHeader(http.StatusOK)
			return
		}
		p2pInfoHandler(w, r, p2pHost, logger, corsConfig)
	})

	// Only wrap base handler with CORS if needed and create optimized middleware
	var finalBaseHandler http.Handler = baseHandler
	if corsConfig != nil {
		finalBaseHandler = createOptimizedCORSMiddleware(baseHandler, corsConfig)
	}

	// For all other paths, use the base handler
	mux.Handle("/", finalBaseHandler)

	return mux
}

// setCORSHeaders sets CORS headers efficiently
func setCORSHeaders(w http.ResponseWriter, corsConfig *CORSConfig) {
	header := w.Header()
	header.Set("Access-Control-Allow-Origin", strings.Join(corsConfig.AllowedOrigins, ", "))
	header.Set("Access-Control-Allow-Methods", strings.Join(corsConfig.AllowedMethods, ", "))
	header.Set("Access-Control-Allow-Headers", strings.Join(corsConfig.AllowedHeaders, ", "))
	header.Set("Access-Control-Expose-Headers", strings.Join(corsConfig.ExposedHeaders, ", "))
}

// createOptimizedCORSMiddleware creates CORS middleware that's optimized for hot paths
func createOptimizedCORSMiddleware(next http.Handler, corsConfig *CORSConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle preflight requests
		if r.Method == http.MethodOptions {
			setCORSHeaders(w, corsConfig)
			w.WriteHeader(http.StatusOK)
			return
		}

		// For actual requests, set headers and continue
		setCORSHeaders(w, corsConfig)
		next.ServeHTTP(w, r)
	})
}

// p2pInfoHandler returns information about the p2p host
func p2pInfoHandler(w http.ResponseWriter, r *http.Request, h host.Host, logger glog.Logger, corsConfig *CORSConfig) {
	// Set CORS headers if configured
	if corsConfig != nil {
		setCORSHeaders(w, corsConfig)
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
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
