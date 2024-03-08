package server

import (
	"fmt"
	"grits/internal/grits"
	"log"
	"sync"
	"time"
)

type Server struct {
	// Core stuff
	Config    *grits.Config
	BlobStore *grits.BlobStore

	// Module stuff
	Modules []Module

	// DHT stuff
	Peers    grits.AllPeers // To store information about known peers
	PeerLock sync.Mutex     // Protects access to Peers

	// Account stuff
	AccountStores map[string]*grits.NameStore
	AccountLock   sync.RWMutex // Protects access to AccountStores

	// Periodic tasks
	taskStop chan struct{}
	taskWg   sync.WaitGroup

	// Long running jobs
	jobs     map[*JobDescriptor]bool
	jobsLock sync.Mutex

	// Shutdown channel
	shutdownChan chan struct{}
	shutdownOnce sync.Once // Ensures shutdown logic runs only once
}

// NewServer initializes and returns a new Server instance.
func NewServer(config *grits.Config) (*Server, error) {
	bs := grits.NewBlobStore(config)
	if bs == nil {
		return nil, fmt.Errorf("failed to initialize blob store")
	}

	srv := &Server{
		Config:        config,
		BlobStore:     bs,
		AccountStores: make(map[string]*grits.NameStore),
		taskStop:      make(chan struct{}),
		jobs:          make(map[*JobDescriptor]bool),
		shutdownChan:  make(chan struct{}),
	}

	err := srv.LoadAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %v", err)
	}

	return srv, nil
}

func (s *Server) Start() error {
	// Load modules from config
	if err := s.LoadModules(s.Config.Modules); err != nil {
		return fmt.Errorf("failed to load modules: %v", err)
	}

	// Start modules
	for _, module := range s.Modules {
		if err := module.Start(); err != nil {
			return fmt.Errorf("failed to start %s module: %v", module.Name(), err)
		}
	}

	// Server periodic tasks
	s.AddPeriodicTask(time.Duration(s.Config.NamespaceSavePeriod)*time.Second, s.SaveAccounts)
	s.AddPeriodicTask(500*time.Millisecond, s.ReportJobs)

	// Start a goroutine to wait for shutdown signal
	go func() {
		<-s.shutdownChan

		s.StopPeriodicTasks()

		err := s.SaveAccounts()
		if err != nil {
			log.Printf("Failed to save accounts: %v\n", err)
		}

		for _, module := range s.Modules {
			if err := module.Stop(); err != nil {
				log.Printf("Error stopping %s module: %v", module.Name(), err)
			}
		}
	}()

	return nil
}

func (s *Server) Stop() {
	s.shutdownOnce.Do(func() {
		close(s.shutdownChan) // Safely close channel
	})
}
