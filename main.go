package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"tripwire/pkg/client"
	"tripwire/pkg/metrics"
	"tripwire/pkg/server"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: ./tripwire run <configFile>")
		os.Exit(1)
	}

	command := os.Args[1]
	if command != "run" {
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}

	zapConf := zap.NewDevelopmentConfig()
	zapConf.EncoderConfig.EncodeTime = zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05")
	log, _ := zapConf.Build()
	logger := log.Sugar()

	configData, err := os.ReadFile(os.Args[2])
	if err != nil {
		logger.Fatalw("failed to read config file", "error", err)
	}
	config, err := parseConfig(configData)
	if err != nil {
		logger.Fatalw("failed to parse config file", "error", err)
	}
	metrics := metrics.New(logger)

	var wg sync.WaitGroup
	if len(config.Client.Workloads) == 0 {
		// Run staged strategies sequentially
		for i, strategy := range config.Strategies {
			if i > 0 {
				time.Sleep(5 * time.Second)
			}
			metrics.Start()
			logger = logger.With("strategy", strategy.Name)
			startClientAndServer(logger, config, strategy, metrics, &wg)
			wg.Wait()
			metrics.Shutdown()
		}
	} else {
		metrics.Start()
		// Run workloads with strategies in parallel
		var clients []*client.Client
		for _, strategy := range config.Strategies {
			logger = logger.With("strategy", strategy.Name)
			aClient, _ := startClientAndServer(logger, config, strategy, metrics, &wg)
			clients = append(clients, aClient)
		}

		configServer := NewConfigServer(clients, logger)
		configServer.Start()
		wg.Wait()
		configServer.Shutdown()
		metrics.Shutdown()
	}
}

func startClientAndServer(logger *zap.SugaredLogger, config *Config, strategy *Strategy, metrics *metrics.Metrics, wg *sync.WaitGroup) (*client.Client, *server.Server) {
	logger.Info("running strategy ", strategy.Name)
	runID := fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), strategy.Name)
	strategyMetrics := metrics.WithStrategy(runID, strategy.Name)
	strategyMetrics.RunDuration.Set(config.Client.MaxDuration.Seconds())

	serverExecutor, _ := strategy.ServerPolicies.ToExecutor(strategyMetrics, logger.Desugar())
	aServer, addr := server.NewServer(config.Server, strategyMetrics, serverExecutor, logger)
	wg.Add(1)
	go aServer.Start(wg)

	clientExecutor, minClientTimeout := strategy.ClientPolicies.ToExecutor(strategyMetrics, logger.Desugar())
	aClient := client.NewClient(addr, config.Client, strategyMetrics, clientExecutor, logger)
	strategyMetrics.MinTimeout.Set(minClientTimeout.Seconds())
	wg.Add(1)
	go aClient.Start(wg)

	return aClient, aServer
}
