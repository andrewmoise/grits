package grits

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBasicLogging(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger()
	logger.Start()
	logger.logger.SetOutput(&buf)

	testMessage := "This is a test message."
	logger.Log("test", testMessage)

	time.Sleep(10 * time.Millisecond)
	logger.Stop()

	assert.Contains(t, buf.String(), testMessage)
}

func TestLoggingAfterStop(t *testing.T) {
	// Backup the original stderr
	originalStderr := os.Stderr

	// Create a reader and writer pair using os.Pipe
	r, w, _ := os.Pipe()

	// Redirect stderr to the writer part of the pipe
	os.Stderr = w

	logger := NewLogger()
	logger.Start()
	logger.Stop()

	testMessage := "This message goes to stderr."
	logger.Log("test", testMessage)

	// Reset stderr back to its original value and close the writer
	os.Stderr = originalStderr
	w.Close()

	// Read from the reader part of the pipe to capture the data written to stderr
	var buf bytes.Buffer
	io.Copy(&buf, r)
	r.Close()

	assert.Contains(t, buf.String(), testMessage)
}

func TestConcurrency(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger()
	logger.Start()
	logger.logger.SetOutput(&buf)

	wg := sync.WaitGroup{}
	n := 100
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			logger.Log("test", fmt.Sprintf("Message %d", id))
		}(i)
	}
	wg.Wait()

	logger.Stop()

	assert.Equal(t, n, len(strings.Split(strings.TrimSpace(buf.String()), "\n")))
}

func TestLogFormatting(t *testing.T) {
	var buf bytes.Buffer

	logger := NewLogger()
	logger.Start()
	logger.logger.SetOutput(&buf)

	testSegment := "testSegment"
	testMessage := "This is a formatted message."
	logger.Log(testSegment, testMessage)
	logger.Stop()

	assert.Contains(t, buf.String(), testSegment)
	assert.Contains(t, buf.String(), testMessage)
}

func TestPanicOnStopWithoutStart(t *testing.T) {
	logger := NewLogger()
	assert.Panics(t, func() {
		logger.Stop()
	})
}
