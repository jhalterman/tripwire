package main

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"
	"gopkg.in/yaml.v3"

	"tripwire/pkg/client"
	"tripwire/pkg/policy"
	"tripwire/pkg/server"
	"tripwire/pkg/util"
)

type Config struct {
	Client      *client.Config `yaml:"client"`
	Server      *server.Config `yaml:"server"`
	Strategies  []*Strategy    `yaml:"strategies"`
	maxDuration time.Duration
}

type Strategy struct {
	Name           string         `yaml:"name"`
	ClientPolicies policy.Configs `yaml:"client_policies"`
	ServerPolicies policy.Configs `yaml:"server_policies"`
}

func (c *Config) isStatic() bool {
	return c.Client.Static != nil && c.Server.Static != nil
}

func parseConfig(configData []byte) (*Config, error) {
	var result Config
	err := yaml.Unmarshal(configData, &result)
	if err != nil {
		return &Config{}, err
	}

	// Find total clients or servers durations
	var clientDuration, serverDuration time.Duration
	var totalDuration time.Duration
	for _, wl := range result.Client.Stages {
		totalDuration += wl.Duration
	}
	clientDuration = totalDuration
	totalDuration = 0
	for _, stage := range result.Server.Stages {
		totalDuration += stage.Duration
	}
	serverDuration = totalDuration
	result.maxDuration = max(clientDuration, serverDuration)

	// Assign clients and servers durations if needed
	for _, stage := range result.Client.Stages {
		if stage.Duration == 0 {
			stage.Duration = serverDuration
		}
	}
	for _, stage := range result.Server.Stages {
		if stage.Duration == 0 {
			stage.Duration = clientDuration
		}
	}

	// Sum servers service time weights
	if result.Server.Stages != nil {
		for _, stage := range result.Server.Stages {
			stage.WeightSum = stage.ServiceTimes.Sum()
		}
	}

	return &result, nil
}

func NewConfigServer(clients []*client.Client, servers []*server.Server, logger *zap.SugaredLogger) *util.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/client", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateClients(clients, w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			updateServers(servers, w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	return util.NewServer(mux, 9095, logger)
}

func updateClients(clients []*client.Client, w http.ResponseWriter, r *http.Request) {
	var config client.Static
	parseConfigUpdate(w, r, &config)
	for _, cl := range clients {
		cl.UpdateStaticConfig(&config)
	}
	fmt.Fprintf(w, "Client config updated successfully\n")
}

func updateServers(servers []*server.Server, w http.ResponseWriter, r *http.Request) {
	var config server.WeightedServiceTimes
	parseConfigUpdate(w, r, &config)
	for _, s := range servers {
		s.UpdateStaticConfig(config)
	}
	fmt.Fprintf(w, "Server config updated successfully\n")
}

func parseConfigUpdate[T any](w http.ResponseWriter, r *http.Request, config T) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	err = yaml.Unmarshal(body, &config)
	if err != nil {
		http.Error(w, "Failed to parse YAML", http.StatusBadRequest)
		return
	}
}
