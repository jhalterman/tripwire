# Tripwire

A client/server load simulator for testing different load scenarios and resilience strategies.

## Usage

Tripwire performs load simulations given configuration for a client, server, and some resilience strategies. Load can be adjusted manually via a REST API, or can run through a sequence of stages.

### Staged Testing

Clients can send requests in one or more stages, which are performed sequentially. This allows load on a server to be gradually increased or decreased over time. An example client config:

```yaml
client:
  stages:
  - rps: 100
    duration: 10s
  - rps: 200
    duration: 20s
  - rps: 100
    duration: 10s
```

Servers can handle requests in one or more stages, which are performed sequentially. This allows a server to simulate changing service times. Each stage contains a weighted service time distribution from which the simulated servicing for each request will be selected, based on the weights, which are optional. Each server also has a fixed number of simulated threads, which represent the max concurrency that the server can support before requests start queueing. An example server config:

```yaml
server:
  stages:
  - duration: 60s
    service_times:
      - service_time: 40ms
        weight: 8
      - service_time: 100ms
        weight: 2
  threads: 8
```

With staged testing, Tripwire will run through a sequence of strategies, consisting of client or server-side policy combinations. This allows you to compare how different strategies perform against some load. An example strategies config:

```yaml
strategies:
  - name: client timeout
    client_policies:
      - timeout: 300ms

  - name: client circuitbreaker and timeout
    client_policies:
      - circuitbreaker:
          failure_rate_threshold: 10
          failure_execution_threshold: 100
          failure_thresholding_period: 5s
          delay: 5s
      - timeout: 300ms
```

See the [configs](configs) directory for example configs.

### Manual Testing

To experiment with different client or server load parameters, you can configure the client and server to use a static config rather than stages:

```yaml
client:
  static:
    rps: 100

server:
  threads: 12
  static:
    - service_time: 50ms
```

Then you can adjust the static client and server config via a REST API:

```sh
curl --request POST --url http://localhost:9095/client \
  --header 'Content-Type: application/yaml' \
  --data 'rps: 200'

curl --request POST --url http://localhost:9095/server \
  --header 'Content-Type: application/yaml' \
  --data '- service_time: 60ms'
```

### More on Strategies

Resilience strategies may consist of [Failsafe-go](https://failsafe-go.dev) policies and [go-concurrency-limits](https://github.com/platinummonkey/go-concurrency-limits) limiters. See the [policy config definitions](https://github.com/jhalterman/tripwire/blob/main/pkg/policy/config.go) for details on how to configure these, and the [configs](configs) directory for example configs.

### Running

To run Tripwire, first build the binary:

```sh
make
```

Then run it with some config file:

```sh
./tripwire run overload.yaml
```

## Dashboard

To observe how scenarios perform in terms of request rates, queueing, concurrency, response times, and load shedding, Tripwire provides a Grafana dashboard with various metrics:

![metrics](./docs/images/metrics.png)

It also includes a summary of how runs for different strategies compare:

![metrics](./docs/images/runs.png)

To start up Grafana and gather metrics from Tripwire:

```sh
docker compose -f deployments/docker-compose.yml up
```

Then you can run simulations and observe them in Grafana: <http://localhost:3000>.