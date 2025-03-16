package gateway

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"strconv"
	"strings"

	"github.com/libp2p/go-libp2p/core/network"
	glog "github.com/omgolab/go-commons/pkg/log"
)

// forwardRequestViaStream forwards an HTTP request through a libp2p stream
// This implements the p2p forwarding part of the flow diagram
func forwardRequestViaStream(w http.ResponseWriter, r *http.Request, stream network.Stream, servicePath string, logger glog.Logger) error {
	// Clone the request to modify it
	req := r.Clone(r.Context())

	// Modify the request path to include the service path
	req.URL.Path = "/" + servicePath
	req.URL.RawPath = "/" + servicePath

	// Remove the host header as it might be incorrect for the destination
	req.Host = ""

	// Set Connect-RPC headers if needed
	if r.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/connect+proto")
	}
	if r.Header.Get("Accept") == "" {
		req.Header.Set("Accept", "application/connect+proto")
	}

	logger.Printf("forwardRequestViaStream - Forwarding to: %s", servicePath)
	logger.Printf("forwardRequestViaStream - Method: %s", req.Method)
	logger.Printf("forwardRequestViaStream - Headers:")
	for k, v := range req.Header {
		logger.Printf("  %s: %v", k, v)
	}

	// Convert the request to raw HTTP
	rawReq, err := httputil.DumpRequestOut(req, true)
	if err != nil {
		return fmt.Errorf("failed to dump request: %w", err)
	}

	logger.Printf("forwardRequestViaStream - Raw request length: %d bytes", len(rawReq))

	// Write the request size prefix (makes it easier to frame the request)
	sizeStr := strconv.Itoa(len(rawReq)) + "\n"
	if _, err := stream.Write([]byte(sizeStr)); err != nil {
		return fmt.Errorf("failed to write request size: %w", err)
	}

	// Write the request to the stream
	if _, err := stream.Write(rawReq); err != nil {
		return fmt.Errorf("failed to write request to stream: %w", err)
	}

	// Read the response size
	reader := bufio.NewReader(stream)
	responseSizeStr, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read response size: %w", err)
	}

	responseSize, err := strconv.Atoi(strings.TrimSpace(responseSizeStr))
	if err != nil {
		return fmt.Errorf("invalid response size: %w", err)
	}

	// Read the response data
	responseData := make([]byte, responseSize)
	totalRead := 0
	for totalRead < responseSize {
		n, err := reader.Read(responseData[totalRead:])
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read response: %w", err)
		}
		totalRead += n
		if err == io.EOF {
			break
		}
	}

	logger.Printf("forwardRequestViaStream - Read %d bytes of response", totalRead)

	// Parse the response
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(responseData)), req)
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}
	defer resp.Body.Close()

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set response status
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}
