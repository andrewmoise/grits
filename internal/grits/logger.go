package grits

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type Logger struct {
	logger       *logrus.Logger
	messageChan  chan string
	shutdownChan chan error
	mux          sync.RWMutex
}

const (
	logBufferSize = 1024
)

func NewLogger() *Logger {
	l := &Logger{
		logger:       nil,
		messageChan:  make(chan string, logBufferSize),
		shutdownChan: make(chan error), // Initialize the channel
	}
	return l
}

func (l *Logger) Log(segmentId, message string) {
	l.mux.RLock()
	if l.logger == nil {
		l.mux.RUnlock()
		fmt.Fprintln(os.Stderr, message)
		return
	}
	l.mux.RUnlock()

	timestamp := time.Now().Format(time.RFC3339)
	if segmentId != "" {
		segmentId = fmt.Sprintf(" %s", segmentId)
	}
	formattedMessage := fmt.Sprintf("%s%s: %s", timestamp, segmentId, message)
	l.messageChan <- formattedMessage
}

func (l *Logger) Start() {
	l.mux.Lock()
	defer l.mux.Unlock()

	if l.logger != nil {
		panic("Logger already started")
	}

	l.logger = logrus.New()
	go writeLogEntries(l, l.logger)
}

func (l *Logger) Stop() {
	l.mux.Lock()
	defer l.mux.Unlock()

	if l.logger == nil {
		panic("Logger not started or already stopped")
	}

	close(l.messageChan)
	l.logger = nil
	<-l.shutdownChan
}

func writeLogEntries(lOuter *Logger, lInner *logrus.Logger) {
	for msg := range lOuter.messageChan {
		lInner.Info(msg)
	}
	lOuter.shutdownChan <- nil // Send nil (or an error if needed)
}
