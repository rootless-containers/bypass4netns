package bypass4netns

import (
	gocontext "context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/containerd/containerd/containers"
	"github.com/containerd/containerd/oci"
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	SocketName = "bypass4netns.sock"
)

func GenerateSecurityOpt(listenerPath string) (oci.SpecOpts, error) {
	opt := func(_ gocontext.Context, _ oci.Client, _ *containers.Container, s *specs.Spec) error {
		s.Linux.Seccomp = getDefaultSeccompProfile(listenerPath)
		return nil
	}
	return opt, nil
}

func getXDGRuntimeDir() (string, error) {
	if xrd := os.Getenv("XDG_RUNTIME_DIR"); xrd != "" {
		return xrd, nil
	}
	return "", fmt.Errorf("environment variable XDG_RUNTIME_DIR is not set")
}

func CreateSocketDir() error {
	xdgRuntimeDir, err := getXDGRuntimeDir()
	if err != nil {
		return err
	}
	dirPath := filepath.Join(xdgRuntimeDir, "bypass4netns")
	if _, err := os.Stat(dirPath); os.IsNotExist(err) {
		err = os.MkdirAll(dirPath, 0775)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetBypass4NetnsdDefaultSocketPath() (string, error) {
	xdgRuntimeDir, err := getXDGRuntimeDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(xdgRuntimeDir, "bypass4netnsd.sock"), nil
}

func GetSocketPathByID(id string) (string, error) {
	xdgRuntimeDir, err := getXDGRuntimeDir()
	if err != nil {
		return "", err
	}

	socketPath := filepath.Join(xdgRuntimeDir, "bypass4netns", id[0:15]+".sock")
	return socketPath, nil
}

func GetPidFilePathByID(id string) (string, error) {
	xdgRuntimeDir, err := getXDGRuntimeDir()
	if err != nil {
		return "", err
	}

	socketPath := filepath.Join(xdgRuntimeDir, "bypass4netns", id[0:15]+".pid")
	return socketPath, nil
}
