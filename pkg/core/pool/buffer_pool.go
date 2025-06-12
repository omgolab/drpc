package pool

import (
	"sync"
	"sync/atomic"
)

// BufferPool provides centralized buffer pooling with metrics
type BufferPool struct {
	pool   *sync.Pool
	size   int
	hits   int64
	misses int64
	gets   int64
	puts   int64
}

// NewBufferPool creates a new buffer pool with the specified buffer size
func NewBufferPool(size int) *BufferPool {
	bp := &BufferPool{
		size: size,
		pool: &sync.Pool{
			New: func() any {
				buf := make([]byte, size)
				return &buf
			},
		},
	}
	return bp
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() *[]byte {
	atomic.AddInt64(&bp.gets, 1)

	if buf, ok := bp.pool.Get().(*[]byte); ok {
		atomic.AddInt64(&bp.hits, 1)
		// Ensure buffer has the correct capacity and length for CopyBuffer
		if cap(*buf) >= bp.size {
			*buf = (*buf)[:bp.size]
			return buf
		}
		// Buffer has insufficient capacity, fall through to create new one
	}

	atomic.AddInt64(&bp.misses, 1)
	buf := make([]byte, bp.size) // Full length, not zero length
	return &buf
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf *[]byte) {
	if buf == nil || cap(*buf) < bp.size {
		return
	}

	atomic.AddInt64(&bp.puts, 1)
	// Reset the buffer to full size for reuse
	*buf = (*buf)[:bp.size]
	bp.pool.Put(buf)
}

// Stats returns pool usage statistics
func (bp *BufferPool) Stats() PoolStats {
	return PoolStats{
		Gets:     atomic.LoadInt64(&bp.gets),
		Puts:     atomic.LoadInt64(&bp.puts),
		Hits:     atomic.LoadInt64(&bp.hits),
		Misses:   atomic.LoadInt64(&bp.misses),
		Size:     bp.size,
		HitRatio: float64(atomic.LoadInt64(&bp.hits)) / float64(atomic.LoadInt64(&bp.gets)),
	}
}

// PoolStats represents buffer pool statistics
type PoolStats struct {
	Gets     int64
	Puts     int64
	Hits     int64
	Misses   int64
	Size     int
	HitRatio float64
}

// Global buffer pools for common sizes
// TODO: can we use a dynamic pool that grows/shrinks based on usage?
// This allows us to avoid creating new buffers for common sizes
// and reuse existing ones, improving performance and reducing memory pressure.
// Try to do in minimal changes to avoid breaking existing code.
// We are trying to do some adaptive pooling in stream_forward.go
var (
	// SmallBufferPool for small operations (4KB)
	// SmallBufferPool = NewBufferPool(4 * 1024)

	// MediumBufferPool for medium operations (32KB)
	MediumBufferPool = NewBufferPool(32 * 1024)

	// LargeBufferPool for large operations (256KB)
	LargeBufferPool = NewBufferPool(256 * 1024)

	// PathBufferPool for URL path parsing (4KB)
	PathBufferPool = NewBufferPool(4 * 1024)

	// ContentTypeBufferPool for content type strings (256 bytes)
	ContentTypeBufferPool = NewBufferPool(256)
)

// ProtobufMessagePool provides pooling for protobuf messages
type ProtobufMessagePool struct {
	pool *sync.Pool
	hits int64
	gets int64
}

// NewProtobufMessagePool creates a new protobuf message pool
func NewProtobufMessagePool(newFunc func() any) *ProtobufMessagePool {
	return &ProtobufMessagePool{
		pool: &sync.Pool{New: newFunc},
	}
}

// Get retrieves a protobuf message from the pool
func (pmp *ProtobufMessagePool) Get() any {
	atomic.AddInt64(&pmp.gets, 1)
	msg := pmp.pool.Get()
	if msg != nil {
		atomic.AddInt64(&pmp.hits, 1)
	}
	return msg
}

// Put returns a protobuf message to the pool
func (pmp *ProtobufMessagePool) Put(msg any) {
	if msg != nil {
		pmp.pool.Put(msg)
	}
}

// Stats returns protobuf pool statistics
func (pmp *ProtobufMessagePool) Stats() ProtobufPoolStats {
	gets := atomic.LoadInt64(&pmp.gets)
	hits := atomic.LoadInt64(&pmp.hits)
	hitRatio := float64(0)
	if gets > 0 {
		hitRatio = float64(hits) / float64(gets)
	}

	return ProtobufPoolStats{
		Gets:     gets,
		Hits:     hits,
		HitRatio: hitRatio,
	}
}

// ProtobufPoolStats represents protobuf pool statistics
type ProtobufPoolStats struct {
	Gets     int64
	Hits     int64
	HitRatio float64
}
