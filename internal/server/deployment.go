package server

// DeploymentConfig represents the configuration for a deployment module.
type DeploymentConfig struct {
	HostName   string `json:"hostName"`
	UrlPath    string `json:"urlPath"`
	Volume     string `json:"volume"`
	VolumePath string `json:"volumePath"`
}

// DeploymentModule represents a deployment module.
type DeploymentModule struct {
	Config *DeploymentConfig
	Server *Server
}

// NewDeploymentModule creates a new instance of DeploymentModule.
func NewDeploymentModule(server *Server, config *DeploymentConfig) *DeploymentModule {
	return &DeploymentModule{
		Config: config,
		Server: server,
	}
}

// Start starts the deployment module.
func (dm *DeploymentModule) Start() error {
	return nil
}

// Stop stops the deployment module.
func (dm *DeploymentModule) Stop() error {
	return nil
}

// GetModuleName returns the name of the deployment module.
func (dm *DeploymentModule) GetModuleName() string {
	return "deployment"
}
