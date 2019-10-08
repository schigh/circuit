package main

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
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
	rand.Seed(time.Now().UnixNano())
	log.SetPrefix("client - ")

	theBox = circuit.NewBreakerBox()

	for {
		select {
		case <-stopChan:
			goto END
		default:
			<-time.After(100 * time.Millisecond)
			breakernames := []string{
				"foo",
				"bar",
				"baz",
				"fizz",
				"buzz",
				"herp",
				"derp",
			}
			breakerName := breakernames[rand.Intn(len(breakernames))]
			breaker, _ := theBox.LoadOrCreate(circuit.BreakerOptions{
				OpeningWillResetErrors: false,
				Threshold:              3,
				Timeout:                10 * time.Millisecond,
				BaudRate:               0,
				BackOff:                10 * time.Second,
				Window:                 10 * time.Second,
				LockOut:                5 * time.Second,
				Name:                   breakerName,
				EstimationFunc:         circuit.Exponential,
			})

			go func(breaker *circuit.Breaker, name string) {
				_, err := breaker.Run(context.Background(), func(ctx context.Context) (interface{}, error) {
					uri := fmt.Sprintf("http://localhost:8080?cb=%s&errchance=%d", name, 9000)
					resp, err := http.DefaultClient.Get(uri)
					if err != nil {
						return []byte(nil), err
					}
					defer resp.Body.Close()
					data, _ := ioutil.ReadAll(resp.Body)
					switch resp.StatusCode {
					case http.StatusOK, http.StatusPartialContent:
						return data, nil
					case http.StatusForbidden:
						log.Printf("throttled on server: '%s'", name)
						return data, errors.New("throttled")
					case http.StatusInternalServerError:
						log.Printf("open on server: '%s'", name)
						return data, errors.New("open")
					}
					log.Println("wat")
					return data, nil
				})

				if err != nil {
					if errors.Is(err, circuit.StateOpenError) {
						log.Printf("open on client: '%s'", name)
					} else if errors.Is(err, circuit.StateThrottledError) {
						log.Printf("throttled on client: '%s'", name)
					} else {
						log.Printf("error from inside breaker '%s': %v", name, err)
					}
				}

			}(breaker, breakerName)
		}
	}

END:
	println("bye")
}
