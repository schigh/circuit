package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/schigh/circuit"
)

func main() {
	//fp, err := os.Create(fmt.Sprintf("%d.csv", time.Now().UnixNano()))
	//csvData := [][]string{
	//	{"ts", "errors", "state", "began", "ends"},
	//}
	//if err != nil {
	//	log.Fatal(err)
	//}
	//defer fp.Close()

	//fn := time.Now().Unix()
	//cp, _ := os.Create(fmt.Sprintf("cpu.%d.prof", fn))
	//mp, _ := os.Create(fmt.Sprintf("mem.%d.prof", fn))
	//defer cp.Close()
	//defer mp.Close()

	//runtime.GC()
	//defer pprof.WriteHeapProfile(mp)
	//pprof.StartCPUProfile(cp)
	//defer pprof.StopCPUProfile()

	//stopChan := make(chan os.Signal, 1)
	//signal.Notify(stopChan, syscall.SIGTERM, syscall.SIGINT)

	execTimer := time.NewTimer(10 * time.Minute)

	b := circuit.NewBreaker(circuit.BreakerOptions{
		Window:    30 * time.Second,
		Threshold: 3,
		LockOut:   5 * time.Second,
		BackOff:   20 * time.Second,
		//OpeningWillResetErrors: true,
		InterpolationFunc: circuit.EaseInOut,
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
	//mx := sync.Mutex{}
	//go func(b *circuit.Breaker, done *uint32, csvData *[][]string, start *time.Time) {
	//	for {
	//		if atomic.LoadUint32(done) == 1 {
	//			return
	//		}
	//		time.Sleep(time.Second)
	//		now := time.Now()
	//		snap := b.Snapshot()
	//		// {"ts", "errors", "state", "began", "ends"},
	//		s := make([]string, 5)
	//		s[0] = now.Format(time.RFC3339)
	//		s[1] = strconv.Itoa(b.Size())
	//		s[2] = strconv.Itoa(int(snap.State))
	//		switch snap.State {
	//		case circuit.Closed:
	//			if snap.ClosedSince != nil {
	//				s[3] = snap.ClosedSince.Format(time.RFC3339)
	//			}
	//		case circuit.Throttled:
	//			if snap.Throttled != nil {
	//				s[3] = snap.Throttled.Format(time.RFC3339)
	//			}
	//			if snap.BackOffEnds != nil {
	//				s[4] = snap.BackOffEnds.Format(time.RFC3339)
	//			}
	//		case circuit.Open:
	//			if snap.Opened != nil {
	//				s[3] = snap.Opened.Format(time.RFC3339)
	//			}
	//			if snap.LockoutEnds != nil {
	//				s[4] = snap.LockoutEnds.Format(time.RFC3339)
	//			}
	//		}
	//		mx.Lock()
	//		*csvData = append(*csvData, s)
	//		mx.Unlock()
	//
	//		fmt.Printf("\n%v\n", now.Sub(*start))
	//
	//		//data, _ := json.MarshalIndent(&snap, "", "  ")
	//		//_, _ = fmt.Printf("\n%s\n", time.Now().Format("15:04:05"))
	//		//_, _ = fmt.Printf("errors: %d\n", b.Size())
	//		//_, _ = fmt.Println(string(data))
	//		//d := time.Now().Sub(*refTime)
	//		//_, _ = fmt.Fprintf(os.Stderr, "%v: size: %d, state: %v\n", d, b.Size(), b.State())
	//	}
	//}(b, &done, &csvData, &start)

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
