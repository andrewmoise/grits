package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"log"
	"net/http"
	"sync"
	"time"
)

type Server struct {
	// Core stuff
	Config    *grits.Config
	BlobStore *grits.BlobStore

	// HTTP stuff
	HTTPServer  *http.Server
	Mux         *http.ServeMux
	DirBackings []*grits.DirBacking

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
		DirBackings: make([]*grits.DirBacking, 0),
		Mux:         http.NewServeMux(),

		AccountStores: make(map[string]*grits.NameStore),
		taskStop:      make(chan struct{}),
	}

	err := srv.LoadAccounts()
	if err != nil {
		return nil, fmt.Errorf("failed to load accounts: %v", err)
	}

	for _, mirror := range config.DirMirrors {
		dirBacking := grits.NewDirBacking(mirror.SourceDir, mirror.CacheLinksDir, bs)
		srv.DirBackings = append(srv.DirBackings, dirBacking)
	}

	srv.setupRoutes()

	return srv, nil
}

func (s *Server) Run() {
	for _, db := range s.DirBackings {
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

	for _, db := range s.DirBackings {
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

func initStore(bs *grits.BlobStore) (*grits.NameStore, error) {
	m := make(map[string]*grits.FileAddr)

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		return nil, err
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		return nil, err
	}

	ns := grits.NewNameStore(rn)
	return ns, nil
}
