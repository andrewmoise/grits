package grits

import (
	"fmt"
	"log"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

/////
// Generic utility and debugging

var DebugRefCounts = false          // Check reference counts when taking/releasing nodes
var DebugBlobStorage = false        // Print periodic detailed stats about the blob store
var VerboseDebugBlobStorage = false // Dump the entire contents of the file store
var DebugNameStore = false          // Debug NameStore main operations
var DebugFileCache = false          // Debug NameStore file cache
var DebugLinks = false              // Debug calls to Link()
var DebugFuse = false               // Debug FUSE mounting
var DebugHttp = false               // Debug HTTP API
var DebugHttpPerformance = true     // Debug HTTP performance
var DebugBlobCache = false          // Debug pass-through LRU blob cache
var DebugServerLifecycle = false    // Debug major server / module events

// More helpers, for performance analysis

func DebugLog(flag bool, format string, v ...any) {
	if flag {
		log.Printf(format, v...)
	}
}

func DebugLogWithTime(flag bool, tag string, format string, v ...any) {
	if flag {
		now := time.Now()
		// Get last 3 digits of seconds (0-999) and milliseconds (0-999)
		secs := now.Second() % 1000 // This just gives 0-59, but keeping your request
		millis := now.Nanosecond() / 1000000

		// Get last 5 characters of tag
		displayTag := tag
		if len(tag) > 5 {
			displayTag = tag[len(tag)-5:]
		}

		prefix := fmt.Sprintf("%.5s %03d.%03d ", displayTag, secs, millis)
		log.Printf(prefix+format, v...)
	}
}

func PrintStack() {
	// Get stack trace info
	pc := make([]uintptr, 10)
	n := runtime.Callers(2, pc) // Skip runtime.Callers and this function
	frames := runtime.CallersFrames(pc[:n])

	// Format caller info
	var callers []string
	for i := 0; i < 15 && frames != nil; i++ {
		frame, more := frames.Next()
		if !more {
			break
		}
		// Extract just the function name and file:line
		funcName := filepath.Base(frame.Function)
		fileName := filepath.Base(frame.File)
		callers = append(callers, fmt.Sprintf("%s() at %s:%d", funcName, fileName, frame.Line))
	}

	// Log the operation
	log.Printf("Stack:\n\t%s\n\n",
		strings.Join(callers, "\n\t"))
}
