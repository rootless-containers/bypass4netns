package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
)

var (
	url  = flag.String("url", "http://localhost/blk-1m", "")
	mode = flag.String("mode", "server", "")
)

func main() {
	flag.Parse()

	// disable connection pool
	http.DefaultTransport.(*http.Transport).MaxIdleConnsPerHost = -1

	if *mode == "server" {
		fmt.Println("starting server")
		server()
	} else if *mode == "client" {
		err := client(*url)
		if err != nil {
			log.Fatal(err)
		}
	}
}

func server() {
	http.HandleFunc("/ping", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "pong")
	})

	log.Fatal(http.ListenAndServe(":8080", nil))
}

func client(url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return fmt.Errorf("failed Do err=%q", err)
	}

	if resp.StatusCode != 200 {
		return fmt.Errorf("unexpected status code %d", resp.StatusCode)
	} else {
		var buffer bytes.Buffer

		_, err = io.Copy(&buffer, resp.Body)
		if err != nil {
			return fmt.Errorf("failed Copy() err=%q", err)
		}

		fmt.Printf("resp=%s\n", buffer.String())
	}
	err = resp.Body.Close()
	if err != nil {
		return fmt.Errorf("failed Close() err=%q", err)
	}

	return nil
}
