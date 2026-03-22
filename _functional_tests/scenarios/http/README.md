# HTTP Functional Test — Circuit Breaker in Action

This scenario demonstrates the circuit breaker library in a realistic microservice environment: an unreliable server, a client generating load, and a full observability stack to visualize circuit breaker behavior in real time.

## Architecture

```
┌────────┐         HTTP         ┌────────┐
│ Client │ ───────────────────► │ Server │
│        │  7 named breakers    │        │
│        │  100ms intervals     │        │──► /metrics (Prometheus format)
└────────┘                      └────────┘
                                     ▲
                                     │ scrape every 5s
                                ┌────┴─────┐
                                │Prometheus │ :9090
                                └────┬─────┘
                                     │
                                ┌────┴─────┐
                                │ Grafana  │ :3000
                                └──────────┘
```

**Server** — An HTTP service that wraps a simulated unreliable dependency with circuit breakers. The dependency fails ~90% of the time. Each unique breaker name (`foo`, `bar`, `baz`, etc.) gets its own independent circuit breaker via `BreakerBox`. The server exposes a `/metrics` endpoint with Prometheus-format counters.

**Client** — A load generator that sends requests to the server every 100ms, randomly selecting from 7 breaker names. The client also wraps outbound calls with its own circuit breakers, demonstrating **cascading circuit breaker protection** — breakers on both sides of the network boundary.

**Prometheus** — Scrapes the server's `/metrics` endpoint every 5 seconds.

**Grafana** — Pre-provisioned with a dashboard showing all circuit breaker metrics. No setup required.

## Running

```bash
cd _functional_tests/scenarios/http
docker compose up --build
```

Wait for the build to complete and services to start. You'll see logs from both the server and client in your terminal.

## What to Observe

### Terminal Logs

Within seconds, you'll see a flurry of activity:

**Server logs** show state transitions as JSON:
```
server - state change: {"name":"foo","state":"open","opened":"2024-...","lockout_ends":"2024-..."}
server - state change: {"name":"foo","state":"throttled","throttled":"2024-...","backoff_ends":"2024-..."}
server - state change: {"name":"bar","state":"open","opened":"2024-...","lockout_ends":"2024-..."}
```

**Client logs** show the client-side breaker reacting to server failures:
```
client - [foo] error: server error 502: error inside circuit breaker 'foo': dependency failure
client - [foo] client breaker OPEN — request rejected
client - [foo] client breaker OPEN — request rejected
client - client breaker 'foo' → throttled
client - [bar] server throttled (excluded from client tracking)
```

### What's Happening

1. **Errors accumulate** — The server's dependency fails 90% of the time. After 5 failures within the 1-minute window, the server-side breaker opens.

2. **Server rejects requests** — While open, the server returns `503 Service Unavailable`. After a 5-second lockout, it enters the throttled state, gradually allowing more requests through using exponential backoff.

3. **Client detects failures** — The client sees 502/503 responses and its own breakers open after 3 failures within 10 seconds.

4. **Client sheds load** — While the client breaker is open, requests are rejected locally without hitting the network. This protects the server from thundering herd during recovery.

5. **Recovery** — After backoff periods expire and error counts drop, breakers transition back to closed. The cycle repeats because the dependency remains 90% unreliable.

6. **Excluded errors** — The client's `IsExcluded` callback treats server-side throttling (`429`) as excluded. These responses don't count against the client's error threshold, preventing the client from opening its own breaker just because the server is recovering.

### Grafana Dashboard

Open [http://localhost:3000](http://localhost:3000) (no login required — anonymous access enabled).

Navigate to **Dashboards → Circuit Breaker Dashboard**, or go directly to:
```
http://localhost:3000/d/circuit-breaker-demo
```

The dashboard has 6 panels:

| Panel | What to Look For |
|-------|-----------------|
| **Circuit Breaker State** | Line chart showing each breaker cycling between 0 (Closed), 1 (Throttled), and 2 (Open). You should see breakers flipping open and recovering independently. |
| **Successes (rate/s)** | Drops to zero when breakers open, gradually recovers during throttle, returns to normal when closed. |
| **Errors (rate/s)** | Spikes that trigger breaker opens. Watch for the rate dropping during open state (errors aren't recorded when the breaker rejects requests). |
| **Rejected Requests (rate/s)** | Stacked bars showing load being shed. High rejection during open state, tapering off during throttle. This is the breaker doing its job. |
| **Timeouts (rate/s)** | Should be near zero in this scenario (the dependency fails fast, not slow). |
| **Total Counters** | Running totals for each breaker — useful for comparing relative activity across breaker names. |

### Prometheus

Open [http://localhost:9090](http://localhost:9090) and try these queries:

```promql
# Current state of all breakers (0=closed, 1=throttled, 2=open)
circuit_breaker_state

# Error rate per breaker over the last 30 seconds
rate(circuit_breaker_errors[30s])

# Ratio of rejected to total requests
rate(circuit_breaker_rejected[30s]) / (rate(circuit_breaker_successes[30s]) + rate(circuit_breaker_errors[30s]) + rate(circuit_breaker_rejected[30s]))

# Which breakers are currently open?
circuit_breaker_state == 2
```

### Key Behaviors to Verify

- [ ] **Independent breakers** — Each of the 7 named breakers opens and recovers on its own schedule. One breaker opening doesn't affect the others.
- [ ] **Lockout works** — After a breaker opens, it stays open for 5 seconds (server-side) before transitioning to throttled, even if errors drop below the threshold.
- [ ] **Gradual recovery** — During the throttled state, the rejection rate decreases over the 10-second backoff period, not all at once.
- [ ] **Error exclusion** — Client-side breakers stay closed longer than server-side ones because server throttling responses are excluded from the client's error count.
- [ ] **No goroutine leaks** — The server's pprof endpoint at [http://localhost:6060/debug/pprof/goroutine?debug=1](http://localhost:6060/debug/pprof/goroutine?debug=1) should show a stable goroutine count (circuit breakers create zero background goroutines).

## Tuning

To experiment with different behaviors, edit the circuit breaker options in the server and client `main.go` files:

| Parameter | Server | Client | Effect of Increasing |
|-----------|--------|--------|---------------------|
| `Threshold` | 5 | 3 | More errors tolerated before opening |
| `Window` | 1m | 10s | Longer error memory |
| `LockOut` | 5s | 5s | Longer forced-open period |
| `BackOff` | 10s | 10s | Slower recovery from throttle |
| `errchance` | 9000 | — | Lower = more server-side errors (inverted: `> errchance` fails) |

To change the error rate without rebuilding, adjust the `errchance` query parameter the client sends (in `client/main.go`, line with `errchance=%d`). Value of `9000` means the server fails when `rand(10000) > 9000`, so ~90% failure rate.

## Stopping

```bash
docker compose down
```

## Ports

| Port | Service |
|------|---------|
| 3000 | Grafana (dashboard) |
| 8080 | Server (API + metrics) |
| 6060 | Server (pprof) |
| 9090 | Prometheus |
