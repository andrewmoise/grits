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
	DirMirrors []grits.DirMirror

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
		Config:    config,
		BlobStore: bs,

		HTTPServer: &http.Server{
			Addr: ":" + fmt.Sprintf("%d", config.ThisPort),
		},
		DirMirrors: make([]grits.DirMirror, 0),
		Mux:        http.NewServeMux(),

		AccountStores: make(map[string]*grits.NameStore),
		taskStop:      make(chan struct{}),
	}

	err := srv.LoadAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %v", err)
	}

	for _, mirrorConfig := range config.DirMirrors {
		switch mirrorConfig.Type {
		case "DirToBlobs":
			dirMirror := grits.NewDirToBlobsMirror(mirrorConfig.SourceDir, mirrorConfig.CacheLinksDir, bs)
			srv.DirMirrors = append(srv.DirMirrors, dirMirror)
		case "DirToTree":
			destPath := mirrorConfig.DestPath
			if strings.HasPrefix(destPath, "/") {
				destPath = destPath[1:]
			}

			dirMirror := grits.NewDirToTreeMirror(mirrorConfig.SourceDir, destPath, bs, srv.AccountStores["root"])
			srv.DirMirrors = append(srv.DirMirrors, dirMirror)
		default:
			return nil, fmt.Errorf("unsupported dir mirror type: %s", mirrorConfig.Type)
		}
	}

	srv.setupRoutes()

	return srv, nil
}

func (s *Server) Run() {
	for _, db := range s.DirMirrors {
		db.Start()
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

	for _, db := range s.DirMirrors {
		db.Stop()
	}
}

func (s *Server) Start() {
	go func() {
		s.Run()
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.HTTPServer.Shutdown(ctx)
}
