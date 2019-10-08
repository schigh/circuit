package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schigh/circuit"
)

var (
	frontErrors uint
)

func main() {
	flag.UintVar(&frontErrors, "front", 0, "front-loaded errors")
	flag.Parse()

	execTimer := time.NewTimer(10 * time.Minute)

	//pp := func(ctx context.Context, i interface{}, err error) (interface{}, error) {
	//	return i, err
	//}

	b := circuit.NewBreaker(circuit.BreakerOptions{
		Window:                 30 * time.Second,
		Threshold:              3,
		LockOut:                5 * time.Second,
		BackOff:                5 * time.Second,
		OpeningWillResetErrors: true,
		EstimationFunc:         circuit.EaseInOut,
		//PostProcessors:         []circuit.PostProcessor{pp},
	})

	stateChange := b.StateChange()
	go func() {
		for {
			select {
			case state := <-stateChange:
				data, _ := json.MarshalIndent(&state, "", "  ")
				_, _ = fmt.Fprintf(os.Stderr, "\n\n%s\n\n", data)
			}
		}
	}()

	done := uint32(0)
	start := time.Now()

	f1 := func(context.Context) (interface{}, error) {
		return nil, nil
	}

	f2 := func(context.Context) (interface{}, error) {
		return nil, errors.New("something happened")
	}

	funcs := []func(context.Context) (interface{}, error){
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f1,
		f1, f1, f1, f1, f1, f1, f1, f1, f1, f2,
	}

	rand.Seed(start.UnixNano())

	for i := 0; i < int(frontErrors); i++ {
		_, _ = b.Run(context.Background(), f2)
	}

	go func(b *circuit.Breaker, done *uint32, refTime *time.Time, funcs []func(context.Context) (interface{}, error)) {
		tick := 1
		for {
			if atomic.LoadUint32(done) == 1 {
				return
			}

			wg := &sync.WaitGroup{}
			iter := 10 //int(rand.Int31n(5))
			wg.Add(iter)

			for i := 0; i < iter; i++ {
				go func(b *circuit.Breaker, funcs []func(context.Context) (interface{}, error), wg *sync.WaitGroup) {
					idx := rand.Int31n(int32(len(funcs)))
					f := funcs[idx]
					_, err := b.Run(context.Background(), f)
					if err != nil {
						switch err {
						case circuit.StateOpenError:
							_, _ = fmt.Fprint(os.Stderr, "🔒")
						case circuit.StateThrottledError:
							_, _ = fmt.Fprint(os.Stderr, "🐢")
						default:
							_, _ = fmt.Fprint(os.Stderr, "💥")
						}
					} else {
						_, _ = fmt.Fprint(os.Stderr, "✅")
					}
					wg.Done()
				}(b, funcs, wg)
			}

			//if b.State() == circuit.Throttled {
			//	time.Sleep(900 * time.Millisecond)
			//}
			wg.Wait()
			time.Sleep(time.Second)
			//time.Sleep(100 * time.Millisecond)
			d := time.Duration(tick) * time.Second
			//d := time.Duration(tick*100) * time.Millisecond
			_, _ = fmt.Fprintf(os.Stderr, "\n%v\tstate: %s\terrors: %d\n", d, b.State().String(), b.Size())
			tick++
		}
	}(b, &done, &start, funcs)

	//<-stopChan
	<-execTimer.C
	atomic.SwapUint32(&done, 1)
	//println("cleaning up")
	//time.Sleep(2 * time.Second)

	//w := csv.NewWriter(fp)
	//w := csv.NewWriter(ioutil.Discard)
	//mx.Lock()
	//w.WriteAll(csvData)
	//mx.Unlock()
}
