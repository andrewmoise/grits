package gritsd

import (
	"sync"
	"time"
)

// Releasable is an interface for anything that needs to be released after use
type Releasable interface {
	Take()
	Release()
}

// heldReference tracks a releasable object and when to release it
type heldReference struct {
	obj       Releasable
	expiresAt time.Time
}

// ReferenceHolder holds onto Releasable objects for a specified duration
// before automatically releasing them
type ReferenceHolder struct {
	pollDuration time.Duration
	
	heldRefs  []heldReference
	refsMutex sync.Mutex
	
	stopWorker chan struct{}
	workerWg   sync.WaitGroup
}

// NewReferenceHolder creates a new ReferenceHolder that will hold references
// for the specified duration before releasing them
func NewReferenceHolder(pollDuration time.Duration) *ReferenceHolder {
	return &ReferenceHolder{
		pollDuration: pollDuration,
		heldRefs:     make([]heldReference, 0),
		stopWorker:   make(chan struct{}),
	}
}

// Start begins the background worker that cleans up expired references
func (rh *ReferenceHolder) Start() {
	rh.workerWg.Add(1)
	go rh.cleanupWorker()
}

// Stop immediately releases all held references and stops the worker
func (rh *ReferenceHolder) Stop() {
	// Signal the worker to stop
	close(rh.stopWorker)
	rh.workerWg.Wait()
	
	// Release all remaining references
	rh.refsMutex.Lock()
	defer rh.refsMutex.Unlock()
	
	for _, ref := range rh.heldRefs {
		ref.obj.Release()
	}
	rh.heldRefs = nil
}

// Hold takes ownership of a Releasable object and will release it after
// the configured hold duration
func (rh *ReferenceHolder) Hold(obj Releasable, holdDuration time.Duration) {
	obj.Take()

	rh.refsMutex.Lock()
	defer rh.refsMutex.Unlock()
	
	expiresAt := time.Now().Add(holdDuration)
	rh.heldRefs = append(rh.heldRefs, heldReference{
		obj:       obj,
		expiresAt: expiresAt,
	})
}

// cleanupWorker runs in a goroutine and periodically releases expired references
func (rh *ReferenceHolder) cleanupWorker() {
	defer rh.workerWg.Done()
	
	ticker := time.NewTicker(rh.pollDuration)
	defer ticker.Stop()
	
	for {
		select {
		case <-rh.stopWorker:
			return
			
		case <-ticker.C:
			rh.cleanupExpired()
		}
	}
}

// cleanupExpired removes and releases all expired references
func (rh *ReferenceHolder) cleanupExpired() {
	now := time.Now()
	
	rh.refsMutex.Lock()
	defer rh.refsMutex.Unlock()
	
	expiredCount := 0
	for _, ref := range rh.heldRefs {
		if now.After(ref.expiresAt) {
			ref.obj.Release()
			expiredCount++
		} else {
			break // Since refs are added in order, rest are not expired
		}
	}
	
	if expiredCount > 0 {
		rh.heldRefs = rh.heldRefs[expiredCount:]
	}
}

// Count returns the number of references currently being held (for debugging/testing)
func (rh *ReferenceHolder) Count() int {
	rh.refsMutex.Lock()
	defer rh.refsMutex.Unlock()
	return len(rh.heldRefs)
}