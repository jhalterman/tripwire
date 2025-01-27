package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

var yamlData = `
client:
  workloads:
    - name: writes
      rps: 100
      service_times:
        - service_time: 50ms
  stages:
    - duration: 20s
      rps: 100
      service_times:
        - service_time: 50ms
    - duration: 40s
      service_times:
        - service_time: 150ms
    - duration: 20s
      service_times:
        - service_time: 50ms

server:
  threads: 8

strategies:
  - name: client timeout
    client_policies:
      - timeout: 300ms

  - name: client rate limiter
    client_policies:
      - ratelimiter:
          rps: 150

  - name: client bulkhead
    client_policies:
      - bulkhead:
          max_concurrency: 8

  - name: client circuitbreaker and timeout
    client_policies:
      - circuitbreaker:
          failure_rate_threshold: 10
          failure_execution_threshold: 100
          failure_thresholding_period: 5s
          delay: 5s
      - timeout: 300ms
`

func TestYAMLParsing(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(yamlData), &config)
	assert.NoError(t, err, "YAML parsing should not return an error")

	// Check Client workloads
	assert.Len(t, config.Client.Workloads, 1)
	assert.Equal(t, "writes", config.Client.Workloads[0].Name)
	assert.Equal(t, uint(100), config.Client.Workloads[0].RPS)
	assert.Equal(t, 50*time.Millisecond, config.Client.Workloads[0].ServiceTimes[0].ServiceTime)

	// Check client stages
	assert.Len(t, config.Client.Stages, 3)
	assert.Equal(t, uint(100), config.Client.Stages[0].RPS)
	assert.Equal(t, 20*time.Second, config.Client.Stages[0].Duration)

	// Check Servers
	assert.Equal(t, uint(8), config.Server.Threads)

	// Check Strategies
	assert.Len(t, config.Strategies, 4)
	assert.Equal(t, "client timeout", config.Strategies[0].Name)
	assert.Equal(t, 300*time.Millisecond, config.Strategies[0].ClientPolicies[0].Timeout)

	assert.Equal(t, "client rate limiter", config.Strategies[1].Name)
	assert.Equal(t, uint(150), config.Strategies[1].ClientPolicies[0].RateLimiterConfig.RPS)

	assert.Equal(t, "client bulkhead", config.Strategies[2].Name)
	assert.Equal(t, uint(8), config.Strategies[2].ClientPolicies[0].BulkheadConfig.MaxConcurrency)

	assert.Equal(t, "client circuitbreaker and timeout", config.Strategies[3].Name)
	assert.Equal(t, uint(10), config.Strategies[3].ClientPolicies[0].CircuitBreakerConfig.FailureRateThreshold)
	assert.Equal(t, 300*time.Millisecond, config.Strategies[3].ClientPolicies[1].Timeout)
}
