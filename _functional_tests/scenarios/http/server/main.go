package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/schigh/circuit"
)

var theBox *circuit.BreakerBox

// --- Prometheus-compatible metrics collector ---

type metricsCollector struct {
	mu         sync.Mutex
	successes  map[string]*atomic.Int64
	errors     map[string]*atomic.Int64
	timeouts   map[string]*atomic.Int64
	rejected   map[string]*atomic.Int64
	excluded   map[string]*atomic.Int64
	stateGauge map[string]*atomic.Int64 // 0=closed, 1=throttled, 2=open
}

func newMetricsCollector() *metricsCollector {
	return &metricsCollector{
		successes:  make(map[string]*atomic.Int64),
		errors:     make(map[string]*atomic.Int64),
		timeouts:   make(map[string]*atomic.Int64),
		rejected:   make(map[string]*atomic.Int64),
		excluded:   make(map[string]*atomic.Int64),
		stateGauge: make(map[string]*atomic.Int64),
	}
}

func (m *metricsCollector) counter(store map[string]*atomic.Int64, name string) *atomic.Int64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := store[name]
	if !ok {
		c = &atomic.Int64{}
		store[name] = c
	}
	return c
}

func (m *metricsCollector) RecordSuccess(name string, _ time.Duration) {
	m.counter(m.successes, name).Add(1)
}
func (m *metricsCollector) RecordError(name string, _ time.Duration, _ error) {
	m.counter(m.errors, name).Add(1)
}
func (m *metricsCollector) RecordTimeout(name string) {
	m.counter(m.timeouts, name).Add(1)
}
func (m *metricsCollector) RecordRejected(name string, _ circuit.State) {
	m.counter(m.rejected, name).Add(1)
}
func (m *metricsCollector) RecordExcluded(name string, _ error) {
	m.counter(m.excluded, name).Add(1)
}
func (m *metricsCollector) RecordStateChange(name string, _, to circuit.State) {
	m.counter(m.stateGauge, name).Store(int64(to))
}

// ServeHTTP exposes metrics in Prometheus exposition format
func (m *metricsCollector) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")

	m.mu.Lock()
	// snapshot the keys
	names := make([]string, 0)
	seen := make(map[string]bool)
	for _, store := range []map[string]*atomic.Int64{m.successes, m.errors, m.timeouts, m.rejected, m.excluded, m.stateGauge} {
		for name := range store {
			if !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
	}
	m.mu.Unlock()

	for _, name := range names {
		fmt.Fprintf(w, "circuit_breaker_successes{breaker=%q} %d\n", name, m.counter(m.successes, name).Load())
		fmt.Fprintf(w, "circuit_breaker_errors{breaker=%q} %d\n", name, m.counter(m.errors, name).Load())
		fmt.Fprintf(w, "circuit_breaker_timeouts{breaker=%q} %d\n", name, m.counter(m.timeouts, name).Load())
		fmt.Fprintf(w, "circuit_breaker_rejected{breaker=%q} %d\n", name, m.counter(m.rejected, name).Load())
		fmt.Fprintf(w, "circuit_breaker_excluded{breaker=%q} %d\n", name, m.counter(m.excluded, name).Load())
		fmt.Fprintf(w, "circuit_breaker_state{breaker=%q} %d\n", name, m.counter(m.stateGauge, name).Load())
	}
}

// --- Server logic ---

var metrics *metricsCollector

func run(ctx context.Context, cbname string, errchance int) (string, int) {
	breaker, _ := theBox.LoadOrCreate(cbname,
		circuit.WithThreshold(5),
		circuit.WithTimeout(time.Second),
		circuit.WithBackOff(10*time.Second),
		circuit.WithWindow(time.Minute),
		circuit.WithLockOut(5*time.Second),
		circuit.WithOpeningResetsErrors(true),
		circuit.WithMetrics(metrics),
	)

	_, err := circuit.Run(breaker, ctx, func(ctx context.Context) (bool, error) {
		// simulate an unreliable dependency
		if rand.IntN(10000) > errchance {
			return false, errors.New("dependency failure")
		}
		return true, nil
	})

	if err != nil {
		switch {
		case errors.Is(err, circuit.ErrStateOpen):
			return fmt.Sprintf("circuit breaker '%s' is open", cbname), http.StatusServiceUnavailable
		case errors.Is(err, circuit.ErrStateThrottled):
			return fmt.Sprintf("circuit breaker '%s' is throttled", cbname), http.StatusTooManyRequests
		case errors.Is(err, circuit.ErrTimeout):
			return fmt.Sprintf("circuit breaker '%s' timed out", cbname), http.StatusGatewayTimeout
		default:
			return fmt.Sprintf("error inside circuit breaker '%s': %v", cbname, err), http.StatusBadGateway
		}
	}
	return "ok", http.StatusOK
}

func createMux() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		cbname := r.URL.Query().Get("cb")
		errchanceStr := r.URL.Query().Get("errchance")
		if cbname == "" || errchanceStr == "" {
			http.Error(w, "cb and errchance query params are required", http.StatusBadRequest)
			return
		}
		errchance, err := strconv.Atoi(errchanceStr)
		if err != nil {
			http.Error(w, "errchance must be an integer", http.StatusBadRequest)
			return
		}

		data, status := run(r.Context(), cbname, errchance)
		w.WriteHeader(status)
		fmt.Fprint(w, data)
	})

	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.Handle("/metrics", metrics)

	return mux
}

func main() {
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	log.SetOutput(os.Stderr)
	log.SetPrefix("server - ")

	metrics = newMetricsCollector()
	theBox = circuit.NewBreakerBox()

	// Log state changes
	go func() {
		for state := range theBox.StateChange() {
			data, _ := json.Marshal(&state)
			log.Printf("state change: %s", data)
		}
	}()

	mux := createMux()
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("server listening on :8080")
		log.Printf("metrics at :8080/metrics")
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	// pprof
	go func() {
		log.Printf("pprof listening on :6060")
		http.ListenAndServe(":6060", nil)
	}()

	<-stopChan
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)
}
