package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type Server struct {
	// Core stuff
	Config    *grits.Config
	BlobStore grits.BlobStore

	// Module stuff
	Modules     []Module
	moduleHooks []func(Module)

	Volumes map[string]Volume

	// Periodic tasks
	taskStop chan struct{}
	taskWg   sync.WaitGroup

	// Long running jobs
	jobs     map[*JobDescriptor]bool
	jobsLock sync.Mutex

	// Shutdown channel
	shutdownChan chan struct{}
	shutdownOnce sync.Once // Ensures shutdown logic runs only once
	shutdownWg   sync.WaitGroup
}

// NewServer initializes and returns a new Server instance.
func NewServer(config *grits.Config) (*Server, error) {
	bs := grits.NewLocalBlobStore(config)
	if bs == nil {
		return nil, fmt.Errorf("failed to initialize blob store")
	}

	err := os.MkdirAll(filepath.Join(config.ServerDir, "var"), 0755)
	if err != nil {
		return nil, fmt.Errorf("cannot create var directory in %s", config.ServerDir)
	}

	srv := &Server{
		Config:       config,
		BlobStore:    bs,
		Volumes:      make(map[string]Volume),
		taskStop:     make(chan struct{}),
		jobs:         make(map[*JobDescriptor]bool),
		shutdownChan: make(chan struct{}),
	}

	return srv, nil
}

func (s *Server) Start() error {
	s.AddPeriodicTask(5*time.Second, s.ReportJobs)

	// Load modules from config
	if err := s.LoadModules(s.Config.Modules); err != nil {
		return fmt.Errorf("failed to load modules: %v", err)
	}

	// Start modules
	for _, module := range s.Modules {
		log.Printf("Starting module %s\n", module.GetModuleName())
		if err := module.Start(); err != nil {
			return fmt.Errorf("failed to start %s module: %v", module.GetModuleName(), err)
		}
	}

	// Start a goroutine to wait for shutdown signal
	go func() {
		<-s.shutdownChan

		s.StopPeriodicTasks()

		for _, module := range s.Modules {
			log.Printf("Stopping module %s\n", module.GetModuleName())
			if err := module.Stop(); err != nil {
				log.Printf("Error stopping %s module: %v", module.GetModuleName(), err)
			}
		}
	}()

	return nil
}

func (s *Server) Stop() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan) // Safely close channel
	})

	// Wait for all shutdown tasks to complete
	s.shutdownWg.Wait()
}

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan) // Safely close channel
	})
}
