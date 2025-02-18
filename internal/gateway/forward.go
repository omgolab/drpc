package gateway

import (
	"fmt"
	"io"
	"net/http"

	glog "github.com/omgolab/go-commons/pkg/log"
)

// forwardRequest forwards the HTTP request to the target service
func forwardRequest(w http.ResponseWriter, r *http.Request, targetAddr, servicePath string, logger glog.Logger) error {
	logger.Printf("forwardRequest - targetAddr: %s", targetAddr)
	logger.Printf("forwardRequest - servicePath: %s", servicePath)

	// Log incoming request details
	logger.Printf("forwardRequest - Method: %s", r.Method)
	logger.Printf("forwardRequest - URL: %s", r.URL.String())
	logger.Printf("forwardRequest - Proto: %s", r.Proto)
	logger.Printf("forwardRequest - Content-Length: %d", r.ContentLength)

	// Log incoming request headers
	logger.Printf("forwardRequest - Incoming Request Headers:")
	for k, v := range r.Header {
		logger.Printf("  %s: %v", k, v)
	}

	// Create a new URL for the target service
	targetURL := fmt.Sprintf("http://%s/%s", targetAddr, servicePath)
	logger.Printf("forwardRequest - targetURL: %s", targetURL)

	// Create new request with the same method, URL, and body
	newReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL, r.Body)
	if err != nil {
		return fmt.Errorf("failed to create new request: %w", err)
	}

	// Copy all headers from original request
	for key, values := range r.Header {
		for _, value := range values {
			newReq.Header.Add(key, value)
		}
	}

	newReq.ContentLength = r.ContentLength

	// Ensure Connect-RPC headers are set correctly
	// newReq.Header.Set("Content-Type", "application/connect+proto")
	// newReq.Header.Set("Accept", "application/connect+proto")
	// newReq.Header.Set("Connect-Protocol-Version", "1")
	// newReq.Header.Set("Connect-Raw-Response", "1")
	// newReq.Header.Set("Accept-Encoding", "identity")
	// newReq.Header.Set("Content-Encoding", "identity")
	// newReq.Header.Set("User-Agent", "connect-go/1.0")
	// newReq.Header.Set("Connect-Timeout-Ms", "15000")

	// Log outgoing request details
	logger.Printf("forwardRequest - Outgoing Request Details:")
	logger.Printf("  Method: %s", newReq.Method)
	logger.Printf("  URL: %s", newReq.URL.String())
	logger.Printf("  Proto: %s", newReq.Proto)
	logger.Printf("  Content-Length: %d", newReq.ContentLength)

	// Log outgoing request headers
	logger.Printf("forwardRequest - Outgoing Request Headers:")
	for k, v := range newReq.Header {
		logger.Printf("  %s: %v", k, v)
	}

	// Create client with transport that supports HTTP/2
	transport := &http.Transport{
		ForceAttemptHTTP2: true,
	}
	client := &http.Client{
		Transport: transport,
	}

	// Send request
	resp, err := client.Do(newReq)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Log response details
	logger.Printf("forwardRequest - Response Status: %s", resp.Status)
	logger.Printf("forwardRequest - Response Headers:")
	for k, v := range resp.Header {
		logger.Printf("  %s: %v", k, v)
	}

	// Copy response headers
	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}

	// Set response status code
	w.WriteHeader(resp.StatusCode)

	// Copy response body
	if _, err := io.Copy(w, resp.Body); err != nil {
		return fmt.Errorf("failed to copy response body: %w", err)
	}

	return nil
}
