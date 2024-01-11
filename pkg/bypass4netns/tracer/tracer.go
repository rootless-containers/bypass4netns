package tracer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"sync"

	"github.com/rootless-containers/bypass4netns/pkg/util"
	"golang.org/x/sys/unix"
)

type Tracer struct {
	logPath   string
	tracerCmd *exec.Cmd
	reader    io.Reader
	writer    io.Writer

	lock sync.Mutex
}

func NewTracer(logPath string) *Tracer {
	return &Tracer{
		logPath: logPath,
		lock:    sync.Mutex{},
	}
}

// StartTracer starts tracer in NS associated with the PID.
func (x *Tracer) StartTracer(ctx context.Context, pid int) error {
	selfExe, err := os.Executable()
	if err != nil {
		return err
	}
	nsenter, err := exec.LookPath("nsenter")
	if err != nil {
		return err
	}
	nsenterFlags := []string{
		"-t", strconv.Itoa(pid),
		"-F",
		"-n",
	}
	selfPid := os.Getpid()
	ok, err := util.SameUserNS(pid, selfPid)
	if err != nil {
		return fmt.Errorf("failed to check sameUserNS(%d, %d)", pid, selfPid)
	}
	if !ok {
		nsenterFlags = append(nsenterFlags, "-U", "--preserve-credentials")
	}
	nsenterFlags = append(nsenterFlags, "--", selfExe, "--tracer-agent", "--log-file", x.logPath)
	x.tracerCmd = exec.CommandContext(ctx, nsenter, nsenterFlags...)
	x.tracerCmd.SysProcAttr = &unix.SysProcAttr{
		Pdeathsig: unix.SIGTERM,
	}
	x.tracerCmd.Stderr = os.Stderr
	x.reader, x.tracerCmd.Stdout = io.Pipe()
	x.tracerCmd.Stdin, x.writer = io.Pipe()
	if err := x.tracerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start %v: %w", x.tracerCmd.Args, err)
	}
	return nil
}

func (x *Tracer) RegisterForwardPorts(ports []int) error {
	cmd := TracerCommand{
		Cmd:             RegisterForwardPorts,
		ForwardingPorts: ports,
	}

	m, err := json.Marshal(cmd)
	if err != nil {
		return err
	}

	writeSize, err := x.writer.Write(m)
	if err != nil {
		return err
	}
	if writeSize != len(m) {
		return fmt.Errorf("unexpected written size expected=%d actual=%d", len(m), writeSize)
	}

	dec := json.NewDecoder(x.reader)
	var resp TracerCommand
	err = dec.Decode(&resp)
	if err != nil {
		return fmt.Errorf("invalid response: %q", err)
	}

	if resp.Cmd != Ok {
		return fmt.Errorf("unexpected response: %d", resp.Cmd)
	}

	return nil
}

func (x *Tracer) ConnectToAddress(addrs []string) ([]string, error) {
	x.lock.Lock()
	defer x.lock.Unlock()

	cmd := TracerCommand{
		Cmd:                ConnectToAddress,
		DestinationAddress: addrs,
	}

	m, err := json.Marshal(cmd)
	if err != nil {
		return nil, err
	}

	writeSize, err := x.writer.Write(m)
	if err != nil {
		return nil, err
	}
	if writeSize != len(m) {
		return nil, fmt.Errorf("unexpected written size expected=%d actual=%d", len(m), writeSize)
	}

	dec := json.NewDecoder(x.reader)
	var resp TracerCommand
	err = dec.Decode(&resp)
	if err != nil {
		return nil, fmt.Errorf("invalid response: %q", err)
	}

	if resp.Cmd != Ok {
		return nil, fmt.Errorf("unexpected response: %d", resp.Cmd)
	}

	return resp.DestinationAddress, nil
}
