package main

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/rootless-containers/bypass4netns/pkg/api"
	"github.com/rootless-containers/bypass4netns/pkg/api/com"
	"github.com/rootless-containers/bypass4netns/pkg/api/daemon/client"
	"github.com/stretchr/testify/assert"
)

// Start bypass4netnsd before testing
func TestNerdctlAPI(t *testing.T) {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}
	client, err := client.New(filepath.Join(xdgRuntimeDir, "bypass4netnsd.sock"))
	if err != nil {
		t.Fatalf("failed client.New %s", err)
	}
	bm := client.BypassManager()
	specs := api.BypassSpec{
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

func TestBypass4netnsAPI(t *testing.T) {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}
	client, err := com.NewComClient(filepath.Join(xdgRuntimeDir, "bypass4netnsd-com.sock"))
	if err != nil {
		t.Fatalf("failed to create ComClient %q", err)
	}

	mac, err := net.ParseMAC("ea:1e:d5:cd:e2:ea")
	assert.Equal(t, nil, err)
	ip, ipNet, err := net.ParseCIDR("10.4.0.53/24")
	assert.Equal(t, nil, err)
	ipNet.IP = ip
	cid := "c70ae35d2aeb4c98c5ef9eb4"
	containerIf := com.ContainerInterfaces{
		ContainerID: cid,
		Interfaces: []com.Interface{
			{
				Name:       "eth0",
				HWAddr:     mac,
				Addresses:  []net.IPNet{*ipNet},
				IsLoopback: false,
			},
		},
		ForwardingPorts: map[int]int{
			5201: 5202,
		},
	}
	err = client.Ping(context.TODO())
	assert.Equal(t, nil, err)

	ifs, err := client.ListInterfaces(context.TODO())
	assert.Equal(t, nil, err)
	assert.Equal(t, 0, len(ifs))

	// this should be error
	_, err = client.GetInterface(context.TODO(), containerIf.ContainerID)
	assert.NotEqual(t, nil, err)

	// Registering interface
	postedIfs, err := client.PostInterface(context.TODO(), &containerIf)
	assert.Equal(t, nil, err)
	assert.Equal(t, postedIfs.ContainerID, containerIf.ContainerID)
	assert.Equal(t, postedIfs.Interfaces[0].HWAddr, containerIf.Interfaces[0].HWAddr)

	ifs2, err := client.ListInterfaces(context.TODO())
	assert.Equal(t, nil, err)
	assert.Equal(t, 1, len(ifs2))
	assert.Equal(t, ifs2[cid].ContainerID, containerIf.ContainerID)
	assert.Equal(t, ifs2[cid].Interfaces[0].HWAddr, containerIf.Interfaces[0].HWAddr)
	assert.Equal(t, ifs2[cid].ForwardingPorts[5201], 5202)

	ifs3, err := client.GetInterface(context.TODO(), containerIf.ContainerID)
	assert.Equal(t, nil, err)
	assert.Equal(t, ifs3.ContainerID, containerIf.ContainerID)
	assert.Equal(t, ifs3.Interfaces[0].HWAddr, containerIf.Interfaces[0].HWAddr)
	assert.Equal(t, ifs3.ForwardingPorts[5201], 5202)

	// Removing interface
	err = client.DeleteInterface(context.TODO(), containerIf.ContainerID)
	assert.Equal(t, nil, err)

	ifs4, err := client.ListInterfaces(context.TODO())
	assert.Equal(t, nil, err)
	assert.Equal(t, 0, len(ifs4))

	_, err = client.GetInterface(context.TODO(), containerIf.ContainerID)
	assert.NotEqual(t, nil, err)
}
