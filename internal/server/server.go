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
	shutdownChan   chan struct{}
	shutdownOnce   sync.Once // Ensures shutdown logic runs only once
	shutdownDoneWg sync.WaitGroup
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

func (s *Server) BlobMaintenance() error {
	// Type assertion to access the enhanced methods we added
	for _, volume := range s.Volumes {
		err := volume.Cleanup()
		if err != nil {
			return err
		}
	}

	if localBS, ok := s.BlobStore.(*grits.LocalBlobStore); ok {
		//log.Println("Running scheduled blob store maintenance...")
		localBS.EvictOldFiles()
	} else {
		log.Println("Blob store doesn't support periodic maintenance")
	}

	return nil
}

func (s *Server) Start() error {
	s.AddPeriodicTask(5*time.Second, s.ReportJobs)

	s.AddPeriodicTask(15*time.Second, s.BlobMaintenance)

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

	s.shutdownDoneWg.Add(1)
	go func() {
		defer s.shutdownDoneWg.Done()

		<-s.shutdownChan

		s.StopPeriodicTasks()

		for i := len(s.Modules) - 1; i >= 0; i-- {
			module := s.Modules[i]
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
	s.shutdownDoneWg.Wait()
}

func (s *Server) Shutdown() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan) // Safely close channel
	})
}
