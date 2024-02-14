package server

import (
	"context"
	"fmt"
	"grits/internal/grits"
	"grits/internal/proxy"
	"net/http"
	"os"
	"sync"
	"time"
)

type Server struct {
	// Core stuff
	Config    *proxy.Config
	BlobStore *proxy.BlobStore
	NameStore *proxy.NameStore

	// HTTP stuff
	HTTPServer  *http.Server
	Mux         *http.ServeMux
	DirBackings []*proxy.DirBacking

	// DHT stuff
	Peers           grits.AllPeers // To store information about known peers
	PeerLock        sync.Mutex     // Protects access to Peers
	HeartbeatTicker *time.Ticker   // Ticker for sending heartbeat
	AnnounceTicker  *time.Ticker   // Ticker for announcing files
}

// NewServer initializes and returns a new Server instance.
func NewServer(config *proxy.Config) (*Server, error) {
	bs := proxy.NewBlobStore(config)
	if bs == nil {
		return nil, fmt.Errorf("Failed to initialize blob store")
	}

	var ns *proxy.NameStore

	// FIXME - Duplicated
	info, err := os.Stat(config.ServerPath("var/namespace_store.json"))
	if err != nil {
		ns, err = initStore(bs, "var/namespace_store.json")
		if err != nil {
			return nil, fmt.Errorf("Failed to initialize namespace store: %v", err)
		}
	} else {
		if info.IsDir() {
			return nil, fmt.Errorf("Namespace store file is a directory")
		}

		ns, err = bs.DeserializeNameStore("var/namespace_store.json")
		if err != nil {
			return nil, fmt.Errorf("Failed to deserialize namespace store: %v", err)
		}
	}

	srv := &Server{
		Config:    config,
		BlobStore: bs,
		NameStore: ns,

		HTTPServer: &http.Server{
			Addr: ":" + fmt.Sprintf("%d", config.ThisPort),
		},
		DirBackings: make([]*proxy.DirBacking, 0),
		Mux:         http.NewServeMux(),
	}

	for _, mirror := range config.DirMirrors {
		dirBacking := proxy.NewDirBacking(mirror.SourceDir, mirror.CacheLinksDir, bs)
		srv.DirBackings = append(srv.DirBackings, dirBacking)
	}

	srv.setupRoutes()
	return srv, nil
}

func (s *Server) Run() error {
	for _, db := range s.DirBackings {
		db.Start()
	}

	err := s.HTTPServer.ListenAndServe()
	if err == http.ErrServerClosed {
		err = nil
	}

	for _, db := range s.DirBackings {
		db.Stop()
	}

	return err
}

func (s *Server) Start() {
	go func() {
		s.Run()
	}()
}

func (s *Server) Stop(ctx context.Context) error {
	return s.HTTPServer.Shutdown(ctx)
}

func initStore(bs *proxy.BlobStore, nameStoreFile string) (*proxy.NameStore, error) {
	m := make(map[string]*grits.FileAddr)

	fn, err := bs.CreateFileNode(m)
	if err != nil {
		return nil, err
	}

	rn, err := bs.CreateRevNode(fn, nil)
	if err != nil {
		return nil, err
	}

	ns := proxy.NewNameStore(rn, nameStoreFile)
	return ns, nil
}
