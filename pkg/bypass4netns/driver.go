package bypass4netns

import (
	"fmt"
	"os"
	"os/exec"
	"sync"

	"github.com/sirupsen/logrus"
)

type Driver struct {
	BypassExecutablePath string
	bypass               map[string]BypassStatus
	lock                 sync.RWMutex
}

type BypassStatus struct {
	ID   string     `json:"id"`
	Pid  int        `json:"pid"`
	Spec BypassSpec `json:"spec"`
}

type BypassSpec struct {
	ID            string     `json:"id"`
	SocketPath    string     `json:"socketPath"`
	PidFilePath   string     `json:"pidFilePath"`
	LogFilePath   string     `json:"logFilePath"`
	PortMapping   []PortSpec `json:"portMapping"`
	IgnoreSubnets []string   `json:"ignoreSubnets"`
}

type PortSpec struct {
	Protos     []string `json:"protos"`
	ParentIP   string   `json:"parentIP"`
	ParentPort int      `json:"parentPort"`
	ChildIP    string   `json:"childIP"`
	ChildPort  int      `json:"childPort"`
}

func NewDriver(execPath string) *Driver {
	return &Driver{
		BypassExecutablePath: execPath,
		bypass:               map[string]BypassStatus{},
		lock:                 sync.RWMutex{},
	}
}

func (d *Driver) ListBypass() []BypassStatus {
	d.lock.RLock()
	defer d.lock.RUnlock()

	res := []BypassStatus{}
	for _, v := range d.bypass {
		res = append(res, v)
	}

	return res
}

func (d *Driver) StartBypass(spec *BypassSpec) (*BypassStatus, error) {
	d.lock.Lock()
	defer d.lock.Unlock()

	b4nsArgs := []string{}

	if spec.SocketPath != "" {
		socketOption := fmt.Sprintf("--socket=%s", spec.SocketPath)
		b4nsArgs = append(b4nsArgs, socketOption)
	}

	if spec.PidFilePath != "" {
		pidFileOption := fmt.Sprintf("--pid-file=%s", spec.PidFilePath)
		b4nsArgs = append(b4nsArgs, pidFileOption)
	}

	if spec.LogFilePath != "" {
		logFileOption := fmt.Sprintf("--log-file=%s", spec.LogFilePath)
		b4nsArgs = append(b4nsArgs, logFileOption)
	}

	for _, port := range spec.PortMapping {
		b4nsArgs = append(b4nsArgs, fmt.Sprintf("-p=%d:%d", port.ParentPort, port.ChildPort))
	}

	for _, subnet := range spec.IgnoreSubnets {
		b4nsArgs = append(b4nsArgs, fmt.Sprintf("--ignore=%s", subnet))
	}

	logrus.Infof("bypass4netns args:%v", b4nsArgs)
	b4nsCmd := exec.Command(d.BypassExecutablePath, b4nsArgs...)
	err := b4nsCmd.Start()
	if err != nil {
		return nil, err
	}

	status := BypassStatus{
		ID:   spec.ID,
		Pid:  b4nsCmd.Process.Pid,
		Spec: *spec,
	}

	d.bypass[status.ID] = status

	return &status, nil
}

func (d *Driver) StopBypass(id string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	bStatus, ok := d.bypass[id]
	if !ok {
		return fmt.Errorf("child %s not found", id)
	}

	proc, err := os.FindProcess(bStatus.Pid)
	if err != nil {
		return err
	}

	err = proc.Kill()
	if err != nil {
		return err
	}

	// wait for the process exit
	// TODO: Timeout
	proc.Wait()

	delete(d.bypass, id)

	return nil
}
