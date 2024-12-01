package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"gopkg.in/yaml.v3"

	"tripwire/pkg/client"
	"tripwire/pkg/policy"
	"tripwire/pkg/server"
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

func parseConfig(configData []byte) (*Config, error) {
	var result Config
	err := yaml.Unmarshal(configData, &result)
	if err != nil {
		return &Config{}, err
	}

	// Find total client or server durations
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

	// Assign client and server durations if needed
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

	// Sum server service time weights
	if result.Server.Stages != nil {
		for _, stage := range result.Server.Stages {
			stage.WeightSum = stage.ServiceTimes.Sum()
		}
	}

	return &result, nil
}

type ConfigServer struct {
	client *client.Client
	server *server.Server
}

func (c *ConfigServer) listen() {
	http.HandleFunc("/client", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			c.getClient(w, r)
		} else if r.Method == http.MethodPost {
			c.updateClient(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})
	http.HandleFunc("/server", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			c.getServer(w, r)
		} else if r.Method == http.MethodPost {
			c.updateServer(w, r)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		}
	})

	log.Println("Config server is running on :9095")
	if err := http.ListenAndServe(":9095", nil); err != nil {
		log.Fatalf("Config server failed to start: %v", err)
	}
}

func (c *ConfigServer) getClient(w http.ResponseWriter, r *http.Request) {
	getConfig(w, c.client.StaticConfig())
}

func (c *ConfigServer) getServer(w http.ResponseWriter, r *http.Request) {
	getConfig(w, c.server.StaticConfig())
}

func getConfig[T any](w http.ResponseWriter, config T) {
	yamlData, err := yaml.Marshal(config)
	if err != nil {
		http.Error(w, "Failed to encode YAML", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/x-yaml")
	w.Write(yamlData)
}

func (c *ConfigServer) updateClient(w http.ResponseWriter, r *http.Request) {
	var config client.Static
	parseConfigUpdate(w, r, &config)
	c.client.UpdateStaticConfig(&config)
	fmt.Fprintf(w, "Client config updated successfully\n")
}

func (c *ConfigServer) updateServer(w http.ResponseWriter, r *http.Request) {
	var config server.WeightedServiceTimes
	parseConfigUpdate(w, r, &config)
	c.server.UpdateStaticConfig(config)
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
