package pool

import (
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
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
func GetPool(h host.Host) *ConnectionPool {
	once.Do(func() {
		defaultInstance = &PoolManager{
			pools: make(map[string]*ConnectionPool),
		}
	})

	return defaultInstance.GetOrCreate(h)
}

// GetOrCreate returns an existing pool for the host or creates a new one
func (pm *PoolManager) GetOrCreate(h host.Host) *ConnectionPool {
	hostID := h.ID().String()

	pm.mu.RLock()
	pool, exists := pm.pools[hostID]
	pm.mu.RUnlock()

	if exists {
		return pool
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Check again to handle race condition
	if pool, exists = pm.pools[hostID]; exists {
		return pool
	}

	// Create new pool
	pool = NewConnectionPool(
		h,
		5*time.Minute, // Maximum idle time
		10,            // Maximum streams per peer
	)
	pm.pools[hostID] = pool

	return pool
}
