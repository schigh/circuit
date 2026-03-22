package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/schigh/circuit"
)

var theBox *circuit.BreakerBox

func main() {
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	log.SetPrefix("client - ")

	serverAddr := os.Getenv("SERVER_ADDR")
	if serverAddr == "" {
		serverAddr = "http://localhost:8080"
	}

	theBox = circuit.NewBreakerBox()

	// Log state changes from all client-side breakers
	go func() {
		for state := range theBox.StateChange() {
			log.Printf("client breaker '%s' → %s", state.Name, state.State)
		}
	}()

	httpClient := &http.Client{Timeout: 5 * time.Second}
	breakerNames := []string{"foo", "bar", "baz", "fizz", "buzz", "herp", "derp"}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	log.Printf("starting load against %s", serverAddr)

	for {
		select {
		case <-stopChan:
			log.Println("shutting down...")
			return
		case <-ticker.C:
			name := breakerNames[rand.IntN(len(breakerNames))]
			go makeRequest(httpClient, serverAddr, name)
		}
	}
}

func makeRequest(httpClient *http.Client, serverAddr, name string) {
	breaker, err := theBox.LoadOrCreate(name,
		circuit.WithThreshold(3),
		circuit.WithTimeout(2*time.Second),
		circuit.WithBackOff(10*time.Second),
		circuit.WithWindow(10*time.Second),
		circuit.WithLockOut(5*time.Second),
		circuit.WithEstimationFunc(circuit.Exponential),
		circuit.WithIsExcluded(func(err error) bool {
			// Don't count server-side throttling against the client breaker
			return errors.Is(err, errServerThrottled)
		}),
	)
	if err != nil {
		log.Printf("failed to get breaker '%s': %v", name, err)
		return
	}

	body, err := circuit.Run(breaker, context.Background(), func(ctx context.Context) (string, error) {
		uri := fmt.Sprintf("%s?cb=%s&errchance=%d", serverAddr, name, 9000)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
		resp, err := httpClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("request failed: %w", err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)

		switch resp.StatusCode {
		case http.StatusOK:
			return string(data), nil
		case http.StatusTooManyRequests:
			return string(data), errServerThrottled
		case http.StatusServiceUnavailable:
			return string(data), fmt.Errorf("server circuit open: %s", data)
		case http.StatusGatewayTimeout:
			return string(data), fmt.Errorf("server timeout: %s", data)
		default:
			return string(data), fmt.Errorf("server error %d: %s", resp.StatusCode, data)
		}
	})

	if err != nil {
		switch {
		case errors.Is(err, circuit.ErrStateOpen):
			log.Printf("[%s] client breaker OPEN — request rejected", name)
		case errors.Is(err, circuit.ErrStateThrottled):
			log.Printf("[%s] client breaker THROTTLED — request shed", name)
		case errors.Is(err, errServerThrottled):
			log.Printf("[%s] server throttled (excluded from client tracking)", name)
		default:
			log.Printf("[%s] error: %v", name, err)
		}
		return
	}

	_ = body // success — quiet in logs to reduce noise
}

var errServerThrottled = errors.New("server throttled")
