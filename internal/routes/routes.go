package routes

import (
	"net/http"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// route names
const (
	p2pInfoRoute = "/p2pinfo"
)

func SetupRoutes(handler http.Handler, logger glog.Logger, p2pHost host.Host) *http.ServeMux {
	mux := http.NewServeMux()
	// configure the gateway handler on the given ServeMux.  Only handle paths starting with /@/*
	mux.Handle("/@/*", getGatewayHandler(logger))

	// configure the P2P info handler on the given ServeMux.
	mux.HandleFunc(p2pInfoRoute, getP2PInfoHandler(p2pHost))

	// let the remaining paths be handled by the given handler
	mux.Handle("/", handler)

	return mux
}
