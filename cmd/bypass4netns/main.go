package main

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
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
	ignoredSubnets := flag.StringSlice("ignore", []string{"127.0.0.0/8"}, "Subnets to ignore in bypass4netns")
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

	subnets := []net.IPNet{}
	for _, subnetStr := range *ignoredSubnets {
		_, subnet, err := net.ParseCIDR(subnetStr)
		if err != nil {
			logrus.Fatalf("%s is not CIDR format", subnetStr)
		}
		subnets = append(subnets, *subnet)
		logrus.Debugf("%s is added to ignore", subnet)
	}

	handler := bypass4netns.NewHandler(socketFile)
	handler.SetIgnoredSubnets(subnets)
	handler.StartHandle()
}
