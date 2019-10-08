package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/schigh/circuit"
)

var theBox *circuit.BreakerBox

func run(ctx context.Context, cbname string, errchance int) (string, int) {
	opts := circuit.BreakerOptions{
		OpeningWillResetErrors: true,
		Threshold:              5,
		Timeout:                time.Second,
		BackOff:                10 * time.Second,
		Window:                 time.Minute,
		LockOut:                5 * time.Second,
		Name:                   cbname,
	}
	breaker, _ := theBox.LoadOrCreate(opts)

	_, err := breaker.Run(ctx, func(ctx context.Context) (interface{}, error) {
		if rand.Intn(10000) > errchance {
			return false, errors.New("broken")
		}

		return true, nil
	})

	if err != nil {
		if errors.Is(err, circuit.StateOpenError) {
			return fmt.Sprintf("circuit breaker '%s' is open", cbname), http.StatusInternalServerError
		} else if errors.Is(err, circuit.StateThrottledError) {
			return fmt.Sprintf("circuit breaker '%s' is throttled", cbname), http.StatusForbidden
		}
		return fmt.Sprintf("an error occured inside circuit breaker '%s'", cbname), http.StatusPartialContent
	}
	return "ok", http.StatusOK
}

func createMuxxer() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		_cbname := r.URL.Query()["cb"]
		_errchance := r.URL.Query()["errchance"]
		if len(_cbname) == 0 || len(_errchance) == 0 {
			http.Error(rw, "cb and errchance are required", http.StatusBadRequest)
			return
		}
		errchance, convErr := strconv.Atoi(_errchance[0])
		if convErr != nil {
			http.Error(rw, "errchance must be an integer", http.StatusBadRequest)
			return
		}
		cbname := _cbname[0]

		data, status := run(r.Context(), cbname, errchance)
		rw.WriteHeader(status)
		_, _ = rw.Write([]byte(data))
	})

	return mux
}

func handleState(state circuit.BreakerState) {
	data, _ := json.MarshalIndent(&state, "", "  ")
	log.Printf("state: %s", data)
}

func main() {
	stopChan := make(chan os.Signal, 1)
	endChan := make(chan struct{})
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)
	log.SetOutput(os.Stderr)
	log.SetPrefix("server - ")
	rand.Seed(time.Now().UnixNano())

	runtime.SetBlockProfileRate(1)
	runtime.SetMutexProfileFraction(1)

	theBox = circuit.NewBreakerBox()

	go func(box *circuit.BreakerBox, endChan chan struct{}) {
		stateChange := box.StateChange()
		for {
			select {
			case state := <-stateChange:
				handleState(state)
			case <-endChan:
				return
			}
		}
	}(theBox, endChan)

	mux := createMuxxer()
	server := &http.Server{
		Addr:         ":8080",
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func(server *http.Server) {
		if err := server.ListenAndServe(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
	}(server)

	pprofServer := &http.Server{
		Addr:         ":6060",
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 60 * time.Second,
	}
	go func(server *http.Server) {
		if err := server.ListenAndServe(); err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
		}
	}(pprofServer)

	<-stopChan
	endChan <- struct{}{}
	_ = server.Close()
	_ = pprofServer.Close()
}
