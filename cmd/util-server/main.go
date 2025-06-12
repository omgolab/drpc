package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // Enable pprof endpoint for memory profiling
	"os"
	"os/signal"
	"runtime"
	"runtime/pprof"
	"sync"
	"syscall"
	"time"

	"github.com/libp2p/go-libp2p"
	gv1connect "github.com/omgolab/drpc/demo/gen/go/greeter/v1/greeterv1connect"
	"github.com/omgolab/drpc/demo/greeter"
	"github.com/omgolab/drpc/pkg/drpc/server"
	"github.com/omgolab/drpc/pkg/gateway"
	glog "github.com/omgolab/go-commons/pkg/log"
)

const DefaultTimeout = 1 * time.Hour // Default timeout for server operations

var (
	logger glog.Logger

	publicNode           *server.DRPCServer
	publicNodeOnce       sync.Once
	publicCachedResponse *NodeResponse

	relayedPrivateNode  *server.DRPCServer
	relayNodeOnce       sync.Once
	relayCachedResponse *NodeResponse

	gatewayNode                    *server.DRPCServer
	gatewayNodeOnce                sync.Once
	gatewayCachedResponse          *NodeResponse
	gatewayRelayNodeOnce           sync.Once
	gatewayRelayCachedResponse     *NodeResponse
	gatewayAutoRelayNodeOnce       sync.Once
	gatewayAutoRelayCachedResponse *NodeResponse

	// pubServeMux will be used by the main publicNode server
	pubServeMux = http.NewServeMux()
)

// Response structures
type NodeResponse struct {
	HTTPAddress string `json:"http_address"`
	Libp2pMA    string `json:"libp2p_ma"`
}

func main() {
	var err error
	logger, err = glog.New()
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Start pprof server for memory profiling
	go func() {
		log.Println("Starting pprof server on :6060")
		if err := http.ListenAndServe("localhost:6060", nil); err != nil {
			logger.Error("pprof server failed", err)
		}
	}()

	// Start memory monitoring goroutine
	go monitorMemoryUsage()

	// Register handlers on the global pubServeMux
	// This mux will be used by the publicNode's HTTP server
	pubServeMux.HandleFunc("/public-node", publicNodeHandler)
	pubServeMux.HandleFunc("/relay-node", relayNodeHandler)
	pubServeMux.HandleFunc("/gateway-node", gatewayNodeHandler)
	pubServeMux.HandleFunc("/gateway-relay-node", gatewayRelayNodeHandler)
	pubServeMux.HandleFunc("/gateway-auto-relay-node", gatewayAutoRelayNodeHandler)

	// Initialize and start the publicNode as the main server.
	initPublicNode()

	// This should be unreachable if the Fatal calls within Once.Do work as expected.
	if publicNode == nil {
		logger.Fatal("Public node is nil after initialization attempt; cannot start server.", fmt.Errorf("publicNode is nil post Once.Do"))
	}

	logger.Info("Endpoints registered: /public-node, /relay-node, /gateway-node, /gateway-relay-node, /gateway-auto-relay-node.")
	// The server.New call starts its HTTP server in a goroutine, so main can proceed to wait for signals.

	// Wait for interrupt signal to gracefully shutdown the server.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.Info("Shutting down server...")

	// Context for graceful shutdown of nodes.
	// Note: Individual Close() methods might not accept this context directly.
	// The effectiveness of this timeout depends on their implementation.
	_, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()

	// Cleanup nodes. Order might matter depending on dependencies, but usually closing them individually is fine.
	if publicNode != nil {
		logger.Info("Closing public node...")
		if err := publicNode.Close(); err != nil {
			logger.Error("Error closing public node", fmt.Errorf("close failed: %v", err))
		}
	}

	if relayedPrivateNode != nil {
		logger.Info("Closing relayed private node...")
		if err := relayedPrivateNode.Close(); err != nil { // host.Host also has a Close method.
			logger.Error("Error closing relayed private node", fmt.Errorf("close failed: %v", err))
		}
	}

	// gatewayNode is the main server. Its Close method should handle shutting down its HTTP listener.
	if gatewayNode != nil {
		logger.Info("Closing gateway node (main server)...")
		if err := gatewayNode.Close(); err != nil {
			logger.Error("Error closing gateway node", fmt.Errorf("close failed: %v", err))
		}
	}

	logger.Info("Server exiting")
}

func initPublicNode() {
	publicNodeOnce.Do(func() {
		path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
		pubServeMux.Handle(path, handler)

		// Public node starts on a dynamic HTTP port and default LibP2P setup
		server, errSetup := server.New(context.Background(), pubServeMux,
			server.WithLogger(logger),
			server.WithHTTPPort(8080),
			server.WithForceCloseExistingPort(true), // Force close if the port is already in use
			// force public reachability since we also intend to use this be used as a public relay node
			server.WithLibP2POptions(libp2p.ForceReachabilityPublic()),
		)
		if errSetup != nil {
			logger.Fatal("Failed to create public node server", errSetup)
		}
		publicNode = server
		httpAddr := publicNode.HTTPAddr()
		var p2pAddr string
		if len(publicNode.P2PAddrs()) > 0 {
			p2pAddr = publicNode.P2PAddrs()[0] // Take the first P2P address
		} else {
			logger.Fatal("Public node P2P address is empty after setup", fmt.Errorf("publicNode.P2PAddrs() is empty"))
		}

		publicCachedResponse = &NodeResponse{
			HTTPAddress: httpAddr,
			Libp2pMA:    p2pAddr,
		}
		logger.Info("Public node initialized", glog.LogFields{"http": httpAddr, "p2p": p2pAddr})
	})
}

func publicNodeHandler(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	initPublicNode() // Ensure publicNode is initialized if not already

	writeJSONResponse(w, "public node", publicCachedResponse)
}

func initRelayNode() {
	relayNodeOnce.Do(func() {

		greeterMux := http.NewServeMux() // Mux for this specific dRPC service
		path, handler := gv1connect.NewGreeterServiceHandler(&greeter.Server{})
		greeterMux.Handle(path, handler)

		// get the public node's P2P address
		initPublicNode() // Ensure publicNode is initialized if not already
		if publicCachedResponse.Libp2pMA == "" {
			logger.Fatal("Public node Libp2pMA is empty; cannot set up relay node.", fmt.Errorf("publicCachedResponse.Libp2pMA is empty"))
		}

		// private node starts on a dynamic HTTP port and default LibP2P setup
		node, errSetup := server.New(context.Background(), greeterMux,
			server.WithDisableHTTP(), // Disable HTTP server for this node
			server.WithLogger(logger),
			// force private reachability since we also intend to stage this be accessible from the public relay node
			server.WithLibP2POptions(libp2p.ForceReachabilityPrivate()),
		)
		if errSetup != nil {
			logger.Fatal("Failed to create relayed private node", errSetup)
		}

		relayedPrivateNode = node
		relayCachedResponse = &NodeResponse{
			HTTPAddress: "", // Relay node itself doesn't need to have an HTTP service endpoint since its private
			Libp2pMA:    publicCachedResponse.Libp2pMA + "/p2p-circuit/p2p/" + relayedPrivateNode.P2PHost().ID().String(),
		}
		logger.Info("Relay node initialized", glog.LogFields{"ma": relayCachedResponse.Libp2pMA})
	})
}

func relayNodeHandler(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	initRelayNode() // Ensure relayedPrivateNode is initialized if not already

	writeJSONResponse(w, "relayed private node", relayCachedResponse)
}

func initGatewayNode() {
	gatewayNodeOnce.Do(func() {

		// This function contains the actual initialization logic for the gateway node.
		// It's called from main() for eager initialization and from gatewayNodeHandler() to ensure initialization.
		server, errSetup := server.New(context.Background(), pubServeMux,
			server.WithLogger(logger),
			// Default libp2p reachability is UNKNOWN; this is left as is intentionally
			// to allow the server to determine its own reachability. In a real-world scenario,
			// this is how you would want to keep the reachability status.
		)
		if errSetup != nil {
			logger.Fatal("Failed to create gateway server", errSetup) // Changed log from "in handler fallback"
		}

		// Ensure publicNode is initialized if not already
		initPublicNode()
		if publicCachedResponse.Libp2pMA == "" {
			logger.Fatal("Public node Libp2pMA is empty; cannot set up gateway node.", fmt.Errorf("publicCachedResponse.Libp2pMA is empty"))
		}

		gatewayNode = server // This assignment is protected by the Once.Do.
		if gatewayNode != nil {
			gatewayCachedResponse = &NodeResponse{
				HTTPAddress: gatewayNode.HTTPAddr() + gateway.GatewayPrefix + publicCachedResponse.Libp2pMA + gateway.GatewayPrefix,
				Libp2pMA:    "",
			}
			logger.Info("Gateway node initialized", glog.LogFields{"http": gatewayNode.HTTPAddr(), "p2p": ""}) // Changed log from "by handler fallback"
		} else {
			logger.Fatal("Gateway node is nil after setup without explicit error", fmt.Errorf("gatewayNode is nil from NewServer")) // Changed log
		}
	})
}

func gatewayNodeHandler(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	initGatewayNode() // Ensure gatewayNode is initialized if not already

	// The check for gatewayCachedResponse == nil is removed here as
	// writeJSONResponse handles nil data. If initGatewayNode() resulted
	// in a fatal error, the program would have already exited.

	writeJSONResponse(w, "gateway node", gatewayCachedResponse)
}

func gatewayRelayNodeHandler(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	gatewayRelayNodeOnce.Do(func() {
		initGatewayNode() // Ensure gatewayNode is initialized if not already
		if gatewayCachedResponse.HTTPAddress == "" {
			logger.Fatal("Gateway node HTTPAddress is empty; cannot set up gateway relay node.", fmt.Errorf("gatewayCachedResponse.HTTPAddress is empty"))
		}

		initRelayNode() // Ensure relayedPrivateNode is initialized if not already
		if relayCachedResponse.Libp2pMA == "" {
			logger.Fatal("Relay node Libp2pMA is empty; cannot set up gateway relay node.", fmt.Errorf("relayCachedResponse.Libp2pMA is empty"))
		}

		gatewayRelayCachedResponse = &NodeResponse{
			HTTPAddress: gatewayNode.HTTPAddr() + gateway.GatewayPrefix + relayCachedResponse.Libp2pMA + gateway.GatewayPrefix,
			Libp2pMA:    "",
		}
		logger.Info("Gateway relay node initialized", glog.LogFields{"http": gatewayRelayCachedResponse.HTTPAddress, "p2p": gatewayRelayCachedResponse.Libp2pMA})
	})

	writeJSONResponse(w, "gateway relay node", gatewayRelayCachedResponse)
}

func gatewayAutoRelayNodeHandler(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Only GET method is allowed", http.StatusMethodNotAllowed)
		return
	}

	gatewayAutoRelayNodeOnce.Do(func() {
		initGatewayNode() // Ensure gatewayNode is initialized if not already
		if gatewayCachedResponse.HTTPAddress == "" {
			logger.Fatal("Gateway node HTTPAddress is empty; cannot set up gateway auto relay node.", fmt.Errorf("gatewayCachedResponse.HTTPAddress is empty"))
		}

		initRelayNode() // Ensure relayedPrivateNode is initialized if not already
		if relayCachedResponse.Libp2pMA == "" {
			logger.Fatal("Relay node Libp2pMA is empty; cannot set up gateway auto relay node.", fmt.Errorf("relayCachedResponse.Libp2pMA is empty"))
		}

		// The gateway node's HTTP address enables access to the relayed private node via the relay.
		// Both gateway and private nodes should be able to discover and use the relay address for connectivity.
		gatewayAutoRelayCachedResponse = &NodeResponse{
			HTTPAddress: gatewayNode.HTTPAddr() + gateway.GatewayPrefix + "/p2p/" + relayedPrivateNode.P2PHost().ID().String() + gateway.GatewayPrefix,
			Libp2pMA:    "",
		}
		logger.Info("Gateway auto relay node initialized", glog.LogFields{"http": gatewayAutoRelayCachedResponse.HTTPAddress, "p2p": gatewayAutoRelayCachedResponse.Libp2pMA})
	})

	writeJSONResponse(w, "gateway auto relay node", gatewayAutoRelayCachedResponse)
}

// monitorMemoryUsage monitors memory usage and detects potential leaks
func monitorMemoryUsage() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	var lastHeapSize uint64
	startTime := time.Now()

	for range ticker.C {
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		currentHeap := m.HeapInuse
		goroutines := runtime.NumGoroutine()
		uptime := time.Since(startTime)

		// Check for potential memory leaks (heap size doubling)
		if currentHeap > lastHeapSize*2 && lastHeapSize > 0 {
			logger.Error("Potential memory leak detected", fmt.Errorf("heap grew from %d to %d bytes in 5 minutes", lastHeapSize, currentHeap))

			// Dump heap profile for analysis
			filename := fmt.Sprintf("heap-leak-%d.prof", time.Now().Unix())
			f, err := os.Create(filename)
			if err == nil {
				pprof.WriteHeapProfile(f)
				f.Close()
				logger.Info("Heap profile dumped", glog.LogFields{"file": filename})
			}
		}

		// Log memory stats periodically
		logger.Info("Memory stats", glog.LogFields{
			"heap_mb":     currentHeap / (1024 * 1024),
			"goroutines":  goroutines,
			"gc_cycles":   m.NumGC,
			"uptime_mins": int(uptime.Minutes()),
		})

		lastHeapSize = currentHeap

		// Alert on high goroutine count (potential goroutine leak)
		if goroutines > 1000 {
			logger.Error("High goroutine count detected", fmt.Errorf("goroutine count: %d", goroutines))
		}
	}
}

// Helper function to write JSON response
func writeJSONResponse(w http.ResponseWriter, nodeName string, data any) {
	if data == nil {
		// This case implies the Once.Do block didn't run or failed to set the response,
		// and publicInitErr was also not set. Should be unlikely if logic is correct.
		// If a fatal error occurred in Do, we wouldn't reach here.
		logger.Error(nodeName+" not initialized and no error reported (cached response is nil)", nil)
		http.Error(w, nodeName+" not initialized and no error reported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		logger.Error(fmt.Sprintf("Failed to encode %s response", nodeName), fmt.Errorf("encode failed: %v", err))
		http.Error(w, fmt.Sprintf("Failed to encode %s response", nodeName), http.StatusInternalServerError)
	}
}
