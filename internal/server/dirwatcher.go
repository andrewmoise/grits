package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"syscall"
)

// DirEventHandler defines the methods required to handle file update events.
type DirEventHandler interface {
	HandleScan(path string) error
}

// DirWatcher watches directory events using a setuid script.
type DirWatcher struct {
	cmd          *exec.Cmd
	scriptPath   string // Path to the setuid script
	watchDir     string
	handler      DirEventHandler
	shutdownFunc func()
}

// NewDirWatcher creates a new DirWatcher.
func NewDirWatcher(script string, watchDir string, handler DirEventHandler, shutdownFunc func()) *DirWatcher {
	return &DirWatcher{
		scriptPath:   script,
		watchDir:     watchDir,
		handler:      handler,
		shutdownFunc: shutdownFunc,
	}
}

// Start begins watching directory changes by executing the setuid script.
func (dw *DirWatcher) Start() error {
	log.Printf("Start DirWatcher\n")

	dw.cmd = exec.Command(dw.scriptPath, "-0", "-g", dw.watchDir)

	stdout, err := dw.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error obtaining stdout pipe: %v", err)
	}

	stderr, err := dw.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error obtaining stderr pipe: %v", err)
	}

	if err := dw.cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %v", err)
	}

	go dw.monitorStderr(stderr)
	go dw.processEvents(stdout)
	go dw.monitorExit()

	return nil
}

func (dw *DirWatcher) processEvents(stdout io.ReadCloser) {
	scanner := bufio.NewScanner(stdout)
	scanner.Split(func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		log.Printf("Got data from watcher proc - %s, %d\n", string(data), len(data))

		if atEOF && len(data) == 0 {
			return 0, nil, nil
		}
		if i := bytes.IndexByte(data, '\x00'); i >= 0 {
			// We have a full null-terminated string
			return i + 1, data[0:i], nil
		}
		// If we're at EOF, we have a final, non-terminated string. Return it.
		if atEOF {
			return len(data), data, nil
		}
		// Request more data.
		return 0, nil, nil
	})

	for scanner.Scan() {
		event := scanner.Text()
		log.Println("Event:", event)
		err := dw.handler.HandleScan(event)
		if err != nil {
			log.Printf("error processing scan event for %s: %v", event, err)
			break
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("Error reading standard input! %v\n", err)
	}

	// Note: We should never get here except on shutdown. If we're not part of an orderly
	// shutdown, there's been a problem -- indicate to whole server to shut down, in
	// that case.
	shutdownFunc := dw.shutdownFunc
	if shutdownFunc != nil {
		shutdownFunc()
	}

	log.Printf("DirWatcher process loop exiting\n")
}

// Stop terminates the directory watching by killing the subprocess.
func (dw *DirWatcher) Stop() error {
	log.Printf("Stop DirWatcher\n")

	// Make sure there's no trigger of the abnormal shutdown case in processEvents().
	dw.shutdownFunc = nil

	if dw.cmd != nil && dw.cmd.Process != nil {
		if err := dw.cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return fmt.Errorf("error sending termination signal: %v", err)
		}
		if _, err := dw.cmd.Process.Wait(); err != nil {
			return fmt.Errorf("error waiting for process to exit: %v", err)
		}
		dw.cmd = nil
	}
	return nil
}

func (dw *DirWatcher) monitorStderr(stderr io.ReadCloser) {
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		log.Printf("From ogwatch: %s\n", scanner.Text())
	}
}

func (dw *DirWatcher) monitorExit() {
	if err := dw.cmd.Wait(); err != nil {
		log.Printf("ogwatch exited with error: %v\n", err)
	} else {
		log.Println("Subprocess exited without error.")
	}
}
