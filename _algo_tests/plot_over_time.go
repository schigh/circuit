package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/schigh/circuit"
)

type rowInfo struct {
	state circuit.BreakerState
	cause string
	die   bool
}

var chance int
var minutes int

func main() {
	flag.IntVar(&chance, "c", 9900, "")
	flag.IntVar(&minutes, "m", 10, "")
	flag.Parse()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	killStateChange := make(chan struct{})
	killBreakers := make(chan struct{})
	writePipe := make(chan rowInfo)
	runlength := time.Duration(minutes) * time.Minute
	clock := time.NewTimer(runlength)

	algos := map[string]circuit.InterpolationFunc{
		"linear":      circuit.Linear,
		"logarithmic": circuit.Logarithmic,
		"exponential": circuit.Exponential,
		"easeinout":   circuit.EaseInOut,
		"sixtypercent": func(int) uint32 {
			return 60
		},
		"fortypercent": func(int) uint32 {
			return 40
		},
		"twentypercent": func(int) uint32 {
			return 20
		},
	}

	var breakerNames []string
	box := circuit.NewBreakerBox()
	for k, f := range algos {
		breakerNames = append(breakerNames, k)
		box.LoadOrCreate(circuit.BreakerOptions{
			Name:              k,
			Timeout:           10 * time.Millisecond,
			BackOff:           time.Minute,
			Window:            time.Minute,
			Threshold:         2,
			LockOut:           5 * time.Second,
			InterpolationFunc: f,
		})
	}

	csvFiles := make(map[string]*os.File)
	defer func(csvFiles map[string]*os.File) {
		for _, f := range csvFiles {
			f.Close()
		}
	}(csvFiles)

	csvWriters := make(map[string]*csv.Writer)
	nowUnix := time.Now().Unix()
	for i := range breakerNames {
		fp, err := os.Create(fmt.Sprintf("%s_%d_%d.csv", breakerNames[i], chance, nowUnix))
		if err != nil {
			log.Fatal(err)
		}
		w := csv.NewWriter(fp)

		// csv headers
		_ = w.Write([]string{
			"ts", "state", "type", "closed_since", "opened", "lockout_ends", "throttled", "backoff_ends",
		})

		csvFiles[breakerNames[i]] = fp
		csvWriters[breakerNames[i]] = w
	}

	// write to csv files
	go func(csvWriters map[string]*csv.Writer, writePipe chan rowInfo) {
		for {
			select {
			case info := <-writePipe:
				if info.die {
					return
				}

				// [0] "ts",
				// [1] "state",
				// [2] "type",
				// [3] "closed_since",
				// [4] "opened",
				// [5] "lockout_ends",
				// [6] "throttled",
				// [7] "backoff_ends",
				row := make([]string, 8)
				row[0] = fmt.Sprintf("%d", time.Now().Unix())
				row[1] = info.state.String()
				row[2] = info.cause
				if info.state.ClosedSince != nil {
					row[3] = fmt.Sprintf("%d", info.state.ClosedSince.Unix())
				}
				if info.state.Opened != nil {
					row[4] = fmt.Sprintf("%d", info.state.Opened.Unix())
				}
				if info.state.LockoutEnds != nil {
					row[5] = fmt.Sprintf("%d", info.state.LockoutEnds.Unix())
				}
				if info.state.Throttled != nil {
					row[6] = fmt.Sprintf("%d", info.state.Throttled.Unix())
				}
				if info.state.BackOffEnds != nil {
					row[7] = fmt.Sprintf("%d", info.state.BackOffEnds.Unix())
				}

				if writer, ok := csvWriters[info.state.Name]; ok {
					_ = writer.Write(row)
					writer.Flush()
				}
			}
		}
	}(csvWriters, writePipe)

	// detect state changes
	go func(stateChange <-chan circuit.BreakerState, csvWriters map[string]*csv.Writer, writePipe chan rowInfo, killStateChange chan struct{}) {
		for {
			select {
			case state := <-stateChange:
				writePipe <- rowInfo{state: state, cause: "state_change"}
			case <-killStateChange:
				return
			}
		}
	}(box.StateChange(), csvWriters, writePipe, killStateChange)

	// run the breakers
	go func(box *circuit.BreakerBox, breakerNames []string, writePipe chan rowInfo, killBreakers chan struct{}) {
		for {
			select {
			case <-killBreakers:
				return
			default:
				{
					for i := range breakerNames {
						breaker := box.Load(breakerNames[i])
						_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
							rando := rand.New(rand.NewSource(time.Now().UnixNano()))
							if rando.Intn(10000) >= chance {
								return nil, errors.New("error")
							}
							return nil, nil
						})
						if err != nil {
							if !errors.Is(err, circuit.StateOpenError) {
								if !errors.Is(err, circuit.StateThrottledError) {
									writePipe <- rowInfo{state: breaker.Snapshot(), cause: "error"}
								}
							}
						}
					}
					time.Sleep(100 * time.Millisecond)
				}
			}
		}
	}(box, breakerNames, writePipe, killBreakers)

	fmt.Printf("running circuit breakers for %s\n", runlength)
	fmt.Printf("this script will stop automatically: %s", time.Now().Add(10*time.Minute).Format(time.Stamp))
	// ----------------------------------------------
	select {
	case <-stop:
		writePipe <- rowInfo{die: true}
		killStateChange <- struct{}{}
		killBreakers <- struct{}{}
	case <-clock.C:
		writePipe <- rowInfo{die: true}
		killStateChange <- struct{}{}
		killBreakers <- struct{}{}
	}

	for _, w := range csvWriters {
		w.Flush()
	}
}
