package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Server struct {
	// Core stuff
	Config    *grits.Config
	BlobStore *grits.BlobStore

	// HTTP stuff
	HTTPServer *http.Server
	Mux        *http.ServeMux
	Volumes    []Volume

	// DHT stuff
	Peers    grits.AllPeers // To store information about known peers
	PeerLock sync.Mutex     // Protects access to Peers

	// Account stuff
	AccountStores map[string]*grits.NameStore
	AccountLock   sync.RWMutex // Protects access to AccountStores

	// Periodic tasks
	taskStop chan struct{}
	taskWg   sync.WaitGroup
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
		HTTPServer:    &http.Server{Addr: ":" + fmt.Sprintf("%d", config.ThisPort)},
		Volumes:       make([]Volume, 0),
		Mux:           http.NewServeMux(),
		AccountStores: make(map[string]*grits.NameStore),
		taskStop:      make(chan struct{}),
	}

	err := srv.LoadAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %v", err)
	}

	for _, volConfig := range config.Volumes {
		var vol Volume
		var err error
		switch volConfig.Type {

		case "LocalDirVolume":
			destPath := volConfig.DestPath
			destPath = strings.TrimPrefix(destPath, "/")

			vol, err = NewDirToTreeMirror(volConfig.SourceDir, destPath, bs, config.DirWatcherPath, srv.Shutdown)

		default:
			return nil, fmt.Errorf("unknown volume type: %s", volConfig.Type)
		}
		if err != nil {
			return nil, fmt.Errorf("failed to create volume: %v", err)
		}

		srv.Volumes = append(srv.Volumes, vol)
	}

	srv.setupRoutes()

	return srv, nil
}

func (s *Server) Run() {
	for _, vol := range s.Volumes {
		vol.Start()
	}

	s.AddPeriodicTask(time.Duration(s.Config.NamespaceSavePeriod)*time.Second, s.SaveAccounts)

	err := s.HTTPServer.ListenAndServe()
	if err == http.ErrServerClosed {
		err = nil
	}
	if err != nil {
		log.Printf("HTTP server error: %v\n", err)
	}

	s.StopPeriodicTasks()

	err = s.SaveAccounts()
	if err != nil {
		log.Printf("Failed to save accounts: %v\n", err)
	}

	for _, vol := range s.Volumes {
		vol.Stop()
	}
}

func (s *Server) Start() {
	go func() {
		s.Run()
	}()
}

// This is called from above us, to indicate, please stop, we're done.
func (s *Server) Stop(ctx context.Context) error {
	return s.HTTPServer.Shutdown(ctx)
}

// This is called from below us, to indicate, oh no, there's a problem, shut down and
// report an error.
func (s *Server) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fmt.Println("Shutting down server...")
	if err := s.Stop(ctx); err != nil {
		log.Printf("Server forced to shutdown: %v\n", err)
	}
}
