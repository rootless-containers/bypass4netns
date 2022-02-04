package main

// This code is copied from 'runc(https://github.com/opencontainers/runc/blob/v1.1.0/contrib/cmd/seccompagent/seccompagent.go)'
// The code is licensed under Apach-2.0 License

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/sirupsen/logrus"
)

var (
	socketFile string
	pidFile    string
)

func main() {
	xdg_runtime_dir := os.Getenv("XDG_RUNTIME_DIR")
	flag.StringVar(&socketFile, "socketfile", xdg_runtime_dir+"/bypass4netns.sock", "Socket file")
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
