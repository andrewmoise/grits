package server

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os/exec"
	"strings"
	"syscall"
)

// DirEventHandler defines the methods required to handle file update events.
type DirEventHandler interface {
	HandleScan(file string) error
	HandleScanTree(directory string) error
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

	log.Printf("%s %s %s %s %s %s %s\n",
		dw.scriptPath, "-0", "-f",
		"FAN_MOVED_TO,FAN_CLOSE_WRITE,FAN_MOVED_FROM,FAN_DELETE",
		"-d", "FAN_MOVED_TO,FAN_MOVED_FROM", dw.watchDir)

	dw.cmd = exec.Command(dw.scriptPath, "-0", "-f",
		"FAN_MOVED_TO,FAN_CLOSE_WRITE,FAN_MOVED_FROM,FAN_DELETE",
		"-d", "FAN_MOVED_TO,FAN_MOVED_FROM", dw.watchDir)

	stdout, err := dw.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error obtaining stdout pipe: %v", err)
	}

	if err := dw.cmd.Start(); err != nil {
		return fmt.Errorf("error starting command: %v", err)
	}

	go dw.processEvents(stdout)

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
		parts := strings.SplitN(event, " ", 2)

		if len(parts) == 1 && parts[0] == "ESTALE" {
			// We ignore this; hopefully that's ok
			//dw.handler.HandleScanTree(dw.watchDir)
		} else if len(parts) == 2 {
			if strings.HasSuffix(parts[0], "|FAN_ONDIR") {
				dw.handler.HandleScanTree(parts[1])
			} else {
				dw.handler.HandleScan(parts[1])
			}
		} else {
			log.Printf("Can't make sense of watcher message! %s\n", event)
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
	}
	return nil
}
