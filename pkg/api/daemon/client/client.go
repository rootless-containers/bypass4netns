// This code is copied from https://github.com/rootless-containers/rootlesskit/blob/master/pkg/api/client/client.go v0.14.6
// The code is licensed under Apache-2.0

package client

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

type Client interface {
	HTTPClient() *http.Client
	BypassManager() *BypassManager
}

// New creates a client.
// socketPath is a path to the UNIX socket, without unix:// prefix.
func New(socketPath string) (Client, error) {
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
	return NewWithHTTPClient(hc), nil
}

func NewWithHTTPClient(hc *http.Client) Client {
	return &client{
		Client:    hc,
		version:   "v1",
		dummyHost: "bypass4netnsd",
	}
}

type client struct {
	*http.Client
	// version is always "v1"
	// TODO(AkihiroSuda): negotiate the version
	version   string
	dummyHost string
}

func (c *client) HTTPClient() *http.Client {
	return c.Client
}

func (c *client) BypassManager() *BypassManager {
	return &BypassManager{
		client: c,
	}
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

type BypassManager struct {
	*client
}

func (bm *BypassManager) StartBypass(ctx context.Context, spec api.BypassSpec) (*api.BypassStatus, error) {
	m, err := json.Marshal(spec)
	if err != nil {
		return nil, err
	}
	u := fmt.Sprintf("http://%s/%s/bypass", bm.client.dummyHost, bm.client.version)
	req, err := http.NewRequest("POST", u, bytes.NewReader(m))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(ctx)
	resp, err := bm.client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(resp.Body)
	var status api.BypassStatus
	if err := dec.Decode(&status); err != nil {
		return nil, err
	}
	return &status, nil
}

func (bm *BypassManager) ListBypass(ctx context.Context) ([]api.BypassStatus, error) {
	u := fmt.Sprintf("http://%s/%s/bypass", bm.client.dummyHost, bm.client.version)
	req, err := http.NewRequest("GET", u, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := bm.client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return nil, err
	}
	var statuses []api.BypassStatus
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&statuses); err != nil {
		return nil, err
	}
	return statuses, nil
}

func (bm *BypassManager) StopBypass(ctx context.Context, id string) error {
	u := fmt.Sprintf("http://%s/%s/bypass/%s", bm.client.dummyHost, bm.client.version, id)
	req, err := http.NewRequest("DELETE", u, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	resp, err := bm.client.HTTPClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := successful(resp); err != nil {
		return err
	}
	return nil
}
