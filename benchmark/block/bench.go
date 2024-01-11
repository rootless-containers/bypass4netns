package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

var (
	url       = flag.String("url", "http://localhost/blk-1m", "")
	threadNum = flag.Int("thread-num", 1, "")
	count     = flag.Int("count", 1, "")
)

type BenchmarkResult struct {
	Url                string  `json:"url"`
	Count              int     `json:"count"`
	TotalElapsedSecond float64 `json:"totalElapsedSecond"`
	TotalSize          int64   `json:"totalSize"`
}

func main() {
	flag.Parse()

	//fmt.Printf("url = %s\n", *url)
	//fmt.Printf("thread-num = %d\n", *threadNum)
	//fmt.Printf("count = %d\n", *count)

	resultsChan := make(chan BenchmarkResult, *count)

	for i := 0; i < *threadNum; i++ {
		go bench(*url, *count, resultsChan)
	}

	results := []BenchmarkResult{}
	for i := 0; i < *threadNum; i++ {
		r := <-resultsChan
		results = append(results, r)
	}

	res, err := json.Marshal(results)
	if err != nil {
		fmt.Printf("failed Marshal err=%q", err)
		panic("error")
	}
	fmt.Fprintln(os.Stdout, string(res))
}

func bench(url string, count int, resultChan chan BenchmarkResult) {
	bufferSize := 1024 * 1024 * 128 // 128 MiB
	buffer := make([]byte, bufferSize)
	result := BenchmarkResult{
		Url:                url,
		Count:              count,
		TotalElapsedSecond: 0,
		TotalSize:          0,
	}

	for i := 0; i < count; i++ {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			fmt.Printf("failed NewRequest err=%q", err)
			panic("error")
		}
		for {
			start := time.Now()
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				fmt.Printf("failed Do err=%q, retrying... %d/%d", err, i, count)
				time.Sleep(100 * time.Millisecond)
				continue
			}

			if resp.StatusCode != 200 {
				fmt.Printf("unexpected status code %d", resp.StatusCode)
				panic("error")
			} else {
				for {
					readSize, err := resp.Body.Read(buffer)
					if err != nil && err != io.EOF {
						fmt.Printf("failed Copy() err=%q", err)
						panic("error")
					}
					if readSize == 0 {
						end := time.Now()
						elapsed := end.Sub(start).Seconds()
						result.TotalElapsedSecond += elapsed
						break
					}
					result.TotalSize += int64(readSize)
				}
			}
			resp.Body.Close()
			break
		}
	}

	resultChan <- result
}
