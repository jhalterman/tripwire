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

	m := metrics.NewMetrics(logger)
	for i, strategy := range config.Strategies {
		m.Reset()
		if i > 0 {
			time.Sleep(5 * time.Second)
		}

		runID := fmt.Sprintf("%s %s", time.Now().Format("15:04:05"), strategy.Name)
		logger.Info("running strategy ", strategy.Name)
		m.Start(runID, config.maxDuration)

		var wg sync.WaitGroup
		wg.Add(1)
		serverExecutor, _ := strategy.ServerPolicies.ToExecutor(m, logger.Desugar())
		aServer := server.NewServer(config.Server, m, serverExecutor, logger)
		go aServer.Start(&wg)

		clientExecutor, minClientTimeout := strategy.ClientPolicies.ToExecutor(m, logger.Desugar())
		m.MinTimeout.Set(minClientTimeout.Seconds())
		wg.Add(1)
		aClient := client.NewClient(config.Client, m, clientExecutor, logger)
		go aClient.Start(&wg)

		if config.Client.Static != nil || config.Server.Static != nil {
			configServer := &ConfigServer{
				client: aClient,
				server: aServer,
			}
			go configServer.listen()
		}

		wg.Wait()
		m.Shutdown()
	}
}
