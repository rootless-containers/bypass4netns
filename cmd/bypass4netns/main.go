package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/sirupsen/logrus"
)

var (
	socketFile string
	pidFile    string
)

func main() {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}

	flag.StringVar(&socketFile, "socket", filepath.Join(xdgRuntimeDir, "bypass4netns.sock"), "Socket file")
	flag.StringVar(&pidFile, "pid-file", "", "Pid file")
	logrus.SetLevel(logrus.DebugLevel)

	// Parse arguments
	flag.Parse()
	if flag.NArg() > 0 {
		flag.PrintDefaults()
		logrus.Fatal("Invalid command")
	}

	if err := os.Remove(socketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logrus.Fatalf("Cannot cleanup socket file: %v", err)
	}

	if pidFile != "" {
		pid := fmt.Sprintf("%d", os.Getpid())
		if err := os.WriteFile(pidFile, []byte(pid), 0o644); err != nil {
			logrus.Fatalf("Cannot write pid file: %v", err)
		}
	}

	handler := bypass4netns.NewHandler(socketFile)
	handler.StartHandle()
}
