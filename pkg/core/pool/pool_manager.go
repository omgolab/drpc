package pool

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	glog "github.com/omgolab/go-commons/pkg/log" // Added import
)

// Default pool configuration
const (
	defaultMaxIdleTime = 5 * time.Minute
	defaultMaxStreams  = 10
)

var (
	// defaultInstance is the global connection pool manager
	defaultInstance *PoolManager
	once            sync.Once
)

// PoolManager manages system-wide connection pools
type PoolManager struct {
	mu    sync.RWMutex
	pools map[string]*ConnectionPool
}

// GetPool returns a connection pool for the given host, creating it if necessary
func GetPool(h host.Host, logger glog.Logger) *ConnectionPool { // Added logger param
	// Initialize singleton instance if not already done
	once.Do(func() {
		defaultInstance = &PoolManager{
			pools: make(map[string]*ConnectionPool),
		}
	})

	return defaultInstance.GetOrCreate(h, logger) // Pass logger
}

// GetOrCreate returns an existing pool for the host or creates a new one
func (pm *PoolManager) GetOrCreate(h host.Host, logger glog.Logger) *ConnectionPool { // Added logger param
	// Cache the host ID string to avoid repeated conversion
	hostID := h.ID().String()

	// Fast path: check with read lock first
	pm.mu.RLock()
	pool, exists := pm.pools[hostID]
	pm.mu.RUnlock()

	if exists {
		return pool
	}

	// Slow path: acquire write lock and create new pool if needed
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check after acquiring write lock to handle race conditions
	if pool, exists = pm.pools[hostID]; exists {
		return pool
	}

	// Create new pool with default configuration
	pool = NewConnectionPool(
		h,
		defaultMaxIdleTime,
		defaultMaxStreams,
		logger, // Pass logger to constructor
	)
	pm.pools[hostID] = pool

	return pool
}
