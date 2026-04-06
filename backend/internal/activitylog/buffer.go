package activitylog

import (
	"sync/atomic"
	"unsafe"
)

// RingBuffer is a lock-free circular buffer for LogEntry.
// It provides non-blocking write and blocking read operations.
// Size must be a power of 2 for efficient masking.
type RingBuffer struct {
	size   uint64
	mask   uint64
	buffer []unsafe.Pointer

	// Read/Write positions (atomic)
	head   uint64 // Next write position
	tail   uint64 // Next read position

	// Dropped count (when buffer full)
	dropped uint64
}

// NewRingBuffer creates a new ring buffer.
// Size must be a power of 2 (e.g., 1024, 2048, 4096, etc.).
// If size is not a power of 2, it will be rounded up to the next power of 2.
func NewRingBuffer(size int) *RingBuffer {
	// Ensure size is power of 2
	if size <= 0 {
		size = 1024
	}
	// Round up to next power of 2
	size--
	size |= size >> 1
	size |= size >> 2
	size |= size >> 4
	size |= size >> 8
	size |= size >> 16
	size++

	return &RingBuffer{
		size:   uint64(size),
		mask:   uint64(size - 1),
		buffer: make([]unsafe.Pointer, size),
	}
}

// Write adds an entry to the buffer (non-blocking).
// Returns true if successful, false if buffer is full (entry dropped).
func (rb *RingBuffer) Write(entry LogEntry) bool {
	for {
		head := atomic.LoadUint64(&rb.head)
		tail := atomic.LoadUint64(&rb.tail)

		// Check if buffer is full
		if head-tail >= rb.size {
			// Buffer full - increment dropped counter
			atomic.AddUint64(&rb.dropped, 1)
			return false
		}

		// Try to claim the slot
		if atomic.CompareAndSwapUint64(&rb.head, head, head+1) {
			// We claimed the slot, write the entry
			idx := head & rb.mask
			ptr := unsafe.Pointer(&entry)
			atomic.StorePointer(&rb.buffer[idx], ptr)
			return true
		}
		// CAS failed, retry
	}
}

// Read removes and returns an entry from the buffer (blocking if empty).
// Returns (entry, true) on success, or (zero, false) if buffer is empty.
func (rb *RingBuffer) Read() (LogEntry, bool) {
	for {
		tail := atomic.LoadUint64(&rb.tail)
		head := atomic.LoadUint64(&rb.head)

		// Check if buffer is empty
		if tail >= head {
			return LogEntry{}, false
		}

		// Try to claim the read slot
		if atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
			// We claimed the slot, read the entry
			idx := tail & rb.mask
			ptr := atomic.LoadPointer(&rb.buffer[idx])
			if ptr == nil {
				// This shouldn't happen with proper synchronization
				continue
			}
			entry := *(*LogEntry)(ptr)
			// Clear the slot to allow GC
			atomic.StorePointer(&rb.buffer[idx], nil)
			return entry, true
		}
		// CAS failed, retry
	}
}

// TryRead attempts to read without blocking.
// Returns (entry, true) if an entry was available, or (zero, false) if empty.
func (rb *RingBuffer) TryRead() (LogEntry, bool) {
	tail := atomic.LoadUint64(&rb.tail)
	head := atomic.LoadUint64(&rb.head)

	if tail >= head {
		return LogEntry{}, false
	}

	// Try to claim the read slot
	if !atomic.CompareAndSwapUint64(&rb.tail, tail, tail+1) {
		return LogEntry{}, false
	}

	idx := tail & rb.mask
	ptr := atomic.LoadPointer(&rb.buffer[idx])
	if ptr == nil {
		return LogEntry{}, false
	}
	entry := *(*LogEntry)(ptr)
	atomic.StorePointer(&rb.buffer[idx], nil)
	return entry, true
}

// Dropped returns the number of entries dropped due to buffer full.
func (rb *RingBuffer) Dropped() uint64 {
	return atomic.LoadUint64(&rb.dropped)
}

// Size returns the current number of entries in the buffer.
func (rb *RingBuffer) Size() uint64 {
	head := atomic.LoadUint64(&rb.head)
	tail := atomic.LoadUint64(&rb.tail)
	if head < tail {
		return 0
	}
	return head - tail
}

// Capacity returns the maximum capacity of the buffer.
func (rb *RingBuffer) Capacity() uint64 {
	return rb.size
}

// IsEmpty returns true if the buffer is empty.
func (rb *RingBuffer) IsEmpty() bool {
	return rb.Size() == 0
}

// IsFull returns true if the buffer is full.
func (rb *RingBuffer) IsFull() bool {
	return rb.Size() >= rb.size
}

// Reset clears the buffer and resets counters.
// Note: This is not thread-safe and should only be called when
// no other goroutines are accessing the buffer.
func (rb *RingBuffer) Reset() {
	atomic.StoreUint64(&rb.head, 0)
	atomic.StoreUint64(&rb.tail, 0)
	atomic.StoreUint64(&rb.dropped, 0)
	for i := range rb.buffer {
		rb.buffer[i] = nil
	}
}

// Drain removes and returns all entries currently in the buffer.
// This is useful for batch processing.
func (rb *RingBuffer) Drain() []LogEntry {
	var entries []LogEntry
	for {
		entry, ok := rb.TryRead()
		if !ok {
			break
		}
		entries = append(entries, entry)
	}
	return entries
}

// DrainN removes and returns up to n entries from the buffer.
func (rb *RingBuffer) DrainN(n int) []LogEntry {
	var entries []LogEntry
	for i := 0; i < n; i++ {
		entry, ok := rb.TryRead()
		if !ok {
			break
		}
		entries = append(entries, entry)
	}
	return entries
}
