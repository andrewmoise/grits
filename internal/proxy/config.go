package proxy

// Config contains the configuration parameters for the proxy instance.
type Config struct {
	// General proxy configuration
	ThisHost      string
	ThisPort      int
	IsRootNode    bool
	RootHost      string
	RootPort      int
	LogFile       string

	// Storage configuration
	StorageDirectory        string
	StorageSize             uint64 // in bytes
	StorageFreeSize         uint64 // in bytes
	TempDownloadDirectory   string

	// DHT params
	DhtNotifyNumber       int
	DhtNotifyPeriod       int // In seconds
	DhtMaxResponseNodes   int
	DhtRefreshTime        int // In seconds
	DhtExpiryTime         int // In seconds

	// Various less-relevant params
	MaxProxyMapAge             int // In seconds
	ProxyMapCleanupPeriod      int // In seconds
	ProxyHeartbeatPeriod       int // In seconds
	RootUpdatePeerListPeriod   int // In seconds
	RootProxyDropTimeout       int // In seconds
}

// NewConfig creates a new configuration instance with default values.
func NewConfig(rootHost string, rootPort int) *Config {
	return &Config{
		ThisHost:                   "127.0.0.1",
		ThisPort:                   1787,
		IsRootNode:                 false,
		RootHost:                   rootHost,
		RootPort:                   rootPort,

		LogFile:                    "grits.log",

		StorageDirectory:           "cache",
		StorageSize:                20 * 1024 * 1024,
		StorageFreeSize:            18 * 1024 * 1024,
		TempDownloadDirectory:      "tmp-download",

		DhtNotifyNumber:            5,
		DhtNotifyPeriod:            20,
		DhtMaxResponseNodes:        10,
		DhtRefreshTime:             8 * 60 * 60,
		DhtExpiryTime:              24 * 60 * 60,

		MaxProxyMapAge:             24 * 60 * 60,
		ProxyMapCleanupPeriod:      60 * 60,
		ProxyHeartbeatPeriod:       10,
		RootUpdatePeerListPeriod:   8,
		RootProxyDropTimeout:       180,
	}
}
