package main

import (
	"fmt"
	"sync"
	"time"

	customHttp2 "github.com/chronnie/http2"
)

const numberOfRequests = 1_000

func main() {
	BenchCustomHttp2()
}

func BenchCustomHttp2() {
	client, err := customHttp2.NewClient("0.0.0.0:1234")
	if err != nil {
		panic(err)
	}
	fmt.Println("Started benchmark...")
	var wg sync.WaitGroup
	timeStart := time.Now()
	for i := 0; i < numberOfRequests; i++ {
		wg.Add(1)
		go func() {
			resp, err := client.GET("/info", "0.0.0.0:1234")
			if err != nil {
				panic(err)
			}
			if resp.StatusCode != 200 {
				panic("unexpected status code")
			}
			wg.Done()
		}()
	}

	wg.Wait()
	fmt.Printf("Send %d requests in %s\n", numberOfRequests, time.Since(timeStart))
}
