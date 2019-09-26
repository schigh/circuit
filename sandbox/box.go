package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/schigh/circuit"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGTERM, syscall.SIGINT)

	box := circuit.NewBreakerBox()
	stateChange := box.StateChange()

	go func() {
		for {
			select {
			case state := <-stateChange:
				data, err := json.MarshalIndent(&state, "", "  ")
				if err != nil {
					_, _ = fmt.Fprintf(os.Stderr, "\n\n%v\n\n", err)
				}
				_, _ = fmt.Fprintf(os.Stderr, "\n\n%s\n\n", data)
			}
		}
	}()

	cb1 := box.LoadOrCreate(circuit.BreakerOptions{
		Name:                   "cb1",
		Timeout:                time.Second,
		BackOff:                10 * time.Second,
		Window:                 30 * time.Second,
		Threshold:              5,
		OpeningWillResetErrors: true,
		LockOut:                5 * time.Second,
	})

	cb2 := box.LoadOrCreate(circuit.BreakerOptions{
		Name:      "cb2",
		Timeout:   time.Second,
		BackOff:   10 * time.Second,
		Window:    time.Minute,
		Threshold: 10,
		LockOut:   5 * time.Second,
	})

	cb3 := box.LoadOrCreate(circuit.BreakerOptions{
		Name:      "cb3",
		Timeout:   time.Second,
		BackOff:   10 * time.Second,
		Window:    2 * time.Minute,
		Threshold: 20,
		LockOut:   5 * time.Second,
	})

	var stop uint32

	go func(cb1, cb2, cb3 *circuit.Breaker, stop *uint32) {
		ctx := context.Background()
		f := func(ctx context.Context) (interface{}, error) {
			if rand.Intn(100) > 80 {
				return nil, errors.New("something happened")
			}
			return nil, nil
		}
		for {
			if atomic.LoadUint32(stop) == 1 {
				return
			}

			_, _ = cb1.Run(ctx, f)
			_, _ = cb2.Run(ctx, f)
			_, _ = cb3.Run(ctx, f)
			time.Sleep(100 * time.Millisecond)
		}
	}(cb1, cb2, cb3, &stop)

	<-stopChan
	atomic.SwapUint32(&stop, 1)
}
