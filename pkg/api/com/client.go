// This code is copied from https://github.com/rootless-containers/rootlesskit/blob/master/pkg/api/client/client.go v0.14.6
// The code is licensed under Apache-2.0

package com

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"

	"github.com/rootless-containers/bypass4netns/pkg/api"
)

type ComClient struct {
	client    *http.Client
	version   string
	dummyHost string
}

func NewComClient(socketPath string) (*ComClient, error) {
	if _, err := os.Stat(socketPath); err != nil {
		return nil, err
	}
	hc := &http.Client{
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				var d net.Dialer
				return d.DialContext(ctx, "unix", socketPath)
			},
		},
	}

	return &ComClient{
		client:    hc,
		version:   "v1",
		dummyHost: "bypass4netnsd-com",
	}, nil
}

func readAtMost(r io.Reader, maxBytes int) ([]byte, error) {
	lr := &io.LimitedReader{
		R: r,
		N: int64(maxBytes),
	}
	b, err := io.ReadAll(lr)
	if err != nil {
		return b, err
	}
	if lr.N == 0 {
		return b, fmt.Errorf("expected at most %d bytes, got more", maxBytes)
	}
	return b, nil
}

// HTTPStatusErrorBodyMaxLength specifies the maximum length of HTTPStatusError.Body
const HTTPStatusErrorBodyMaxLength = 64 * 1024

// HTTPStatusError is created from non-2XX HTTP response
type HTTPStatusError struct {
	// StatusCode is non-2XX status code
	StatusCode int
	// Body is at most HTTPStatusErrorBodyMaxLength
	Body string
}

// Error implements error.
// If e.Body is a marshalled string of api.ErrorJSON, Error returns ErrorJSON.Message .
// Otherwise Error returns a human-readable string that contains e.StatusCode and e.Body.
func (e *HTTPStatusError) Error() string {
	if e.Body != "" && len(e.Body) < HTTPStatusErrorBodyMaxLength {
		var ej api.ErrorJSON
		if json.Unmarshal([]byte(e.Body), &ej) == nil {
			return ej.Message
		}
	}
	return fmt.Sprintf("unexpected HTTP status %s, body=%q", http.StatusText(e.StatusCode), e.Body)
}

func successful(resp *http.Response) error {
	if resp == nil {
		return errors.New("nil response")
	}
	if resp.StatusCode/100 != 2 {
		b, _ := readAtMost(resp.Body, HTTPStatusErrorBodyMaxLength)
		return &HTTPStatusError{
			StatusCode: resp.StatusCode,
			Body:       string(b),
		}
	}
	return nil
}

func (c *ComClient) Ping(ctx context.Context) error {
	m, err := json.Marshal("ping")
	if err != nil {
		return err
	}
	u := fmt.Sprintf("http://%s/%s/ping", c.dummyHost, c.version)
	req, err := http.NewRequest("GET", u, bytes.NewReader(m))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return err
	}
	dec := json.NewDecoder(resp.Body)
	var pong string
	if err := dec.Decode(&pong); err != nil {
		return err
	}

	if pong != "pong" {
		return fmt.Errorf("unexpected response expected=%q actual=%q", "pong", pong)
	}
	return nil
}

func (c *ComClient) ListInterfaces(ctx context.Context) (map[string]ContainerInterfaces, error) {
	u := fmt.Sprintf("http://%s/%s/interfaces", c.dummyHost, c.version)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(resp.Body)
	var containerIfs map[string]ContainerInterfaces
	if err := dec.Decode(&containerIfs); err != nil {
		return nil, err
	}

	return containerIfs, nil
}

func (c *ComClient) GetInterface(ctx context.Context, id string) (*ContainerInterfaces, error) {
	u := fmt.Sprintf("http://%s/%s/interface/%s", c.dummyHost, c.version, id)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(resp.Body)
	var containerIfs ContainerInterfaces
	if err := dec.Decode(&containerIfs); err != nil {
		return nil, err
	}

	return &containerIfs, nil
}

func (c *ComClient) PostInterface(ctx context.Context, ifs *ContainerInterfaces) (*ContainerInterfaces, error) {
	m, err := json.Marshal(ifs)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("http://%s/%s/interface/%s", c.dummyHost, c.version, ifs.ContainerID)
	req, err := http.NewRequest("POST", u, bytes.NewReader(m))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(resp.Body)
	var containerIfs ContainerInterfaces
	if err := dec.Decode(&containerIfs); err != nil {
		return nil, err
	}

	return &containerIfs, nil
}

func (c *ComClient) DeleteInterface(ctx context.Context, id string) error {
	u := fmt.Sprintf("http://%s/%s/interface/%s", c.dummyHost, c.version, id)
	req, err := http.NewRequest("DELETE", u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return err
	}
	return nil
}
