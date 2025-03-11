package server

import (
	"grits/internal/grits"
	"log"
	"net/http"
	"time"
)

// PerformanceTracker helps track the execution time of different steps in request handling
type PerformanceTracker struct {
	startTime time.Time
	lastTime  time.Time
	enabled   bool
	url       string
	method    string
}

// NewPerformanceTracker creates a new performance tracker
func NewPerformanceTracker(r *http.Request) *PerformanceTracker {
	now := time.Now()
	return &PerformanceTracker{
		startTime: now,
		lastTime:  now,
		enabled:   grits.DebugHttpPerformance,
		url:       r.URL.Path,
		method:    r.Method,
	}
}

// Start begins tracking with an initial message
func (pt *PerformanceTracker) Start() {
	if !pt.enabled {
		return
	}

	log.Printf("%s: Got %s request for %s",
		pt.startTime.Format("15:04:05.000"),
		pt.method,
		pt.url)
}

// Step logs the time taken for a step and updates the last time
func (pt *PerformanceTracker) Step(message string) {
	if !pt.enabled {
		return
	}

	now := time.Now()
	elapsed := now.Sub(pt.lastTime)

	log.Printf("  +%dms: %s",
		elapsed.Milliseconds(),
		message)

	pt.lastTime = now
}

// End logs the total time taken for the request
func (pt *PerformanceTracker) End() {
	if !pt.enabled {
		return
	}

	total := time.Since(pt.startTime)
	log.Printf("  Total: %dms for %s %s",
		total.Milliseconds(),
		pt.method,
		pt.url)
}
