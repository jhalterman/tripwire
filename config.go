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
	Client     *client.Config `yaml:"client"`
	Server     *server.Config `yaml:"server"`
	Strategies []*Strategy    `yaml:"strategies"`
}

type Strategy struct {
	Name           string         `yaml:"name"`
	ClientPolicies policy.Configs `yaml:"client_policies"`
	ServerPolicies policy.Configs `yaml:"server_policies"`
}

func parseConfig(configData []byte) (*Config, error) {
	var result Config
	err := yaml.Unmarshal(configData, &result)
	if err != nil {
		return &Config{}, err
	}

	configureWorkloads(result.Client.Workloads)
	var previousStage *client.Stage
	for _, stage := range result.Client.Stages {
		// Carry over RPS and service times from one stage to another if needed
		if previousStage != nil {
			if stage.RPS == 0 {
				stage.RPS = previousStage.RPS
			}
			if stage.ServiceTimes == nil {
				stage.ServiceTimes = previousStage.ServiceTimes
			}
		}
		result.Client.MaxDuration += stage.Duration
		stage.WeightSum = int(stage.ServiceTimes.Sum())
		previousStage = stage
	}
	if result.Client.MaxDuration != 0 {
		result.Server.Duration = result.Client.MaxDuration
	} else {
		result.Server.Duration = 24 * time.Hour
	}

	return &result, nil
}

func configureWorkloads(workloads []*client.Workload) {
	for _, workload := range workloads {
		workload.WeightSum = int(workload.ServiceTimes.Sum())
	}
}

func NewConfigServer(clients []*client.Client, servers []*server.Server, logger *zap.SugaredLogger) *util.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("/client/workloads", func(w http.ResponseWriter, r *http.Request) {
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
	var workloads []*client.Workload
	if parseConfigUpdate(w, r, &workloads) {
		configureWorkloads(workloads)
		for _, cl := range clients {
			cl.UpdateWorkloads(workloads)
		}
		fmt.Fprintf(w, "Client config updated successfully\n")
	}
}

func updateServers(servers []*server.Server, w http.ResponseWriter, r *http.Request) {
	var config *server.Config
	if parseConfigUpdate(w, r, &config) {
		for _, srv := range servers {
			srv.UpdateConfig(config)
		}
		fmt.Fprintf(w, "Server config updated successfully\n")
	}
}

func parseConfigUpdate[T any](w http.ResponseWriter, r *http.Request, config T) bool {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return false
	}
	defer r.Body.Close()
	err = yaml.Unmarshal(body, &config)
	if err != nil {
		http.Error(w, "Failed to parse YAML: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}
