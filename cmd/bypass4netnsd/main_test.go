package main

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/rootless-containers/bypass4netns/pkg/api/daemon/client"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/stretchr/testify/assert"
)

// Start bypass4netnsd before testing
func TestBypass4netnsd(t *testing.T) {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}
	client, err := client.New(filepath.Join(xdgRuntimeDir, "bypass4netnsd.sock"))
	if err != nil {
		t.Fatalf("failed client.New %s", err)
	}
	bm := client.BypassManager()
	specs := bypass4netns.BypassSpec{
		ID: "1234567890",
	}
	status, err := bm.StartBypass(context.TODO(), specs)
	assert.Equal(t, nil, err)

	statuses, err := bm.ListBypass(context.TODO())
	assert.Equal(t, nil, err)
	assert.Equal(t, 1, len(statuses))
	newStatus := statuses[0]
	assert.Equal(t, status.ID, newStatus.ID)
	assert.NotEqual(t, 0, newStatus.Pid)
	assert.Equal(t, true, isProcessRunning(newStatus.Pid))

	err = bm.StopBypass(context.TODO(), specs.ID)
	assert.Equal(t, nil, err)
	assert.Equal(t, false, isProcessRunning(newStatus.Pid))

	statuses, err = bm.ListBypass(context.TODO())
	assert.Equal(t, nil, err)
	assert.Equal(t, 0, len(statuses))
}

func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// check the process is alive or not
	err = proc.Signal(syscall.Signal(0))

	return err == nil
}
