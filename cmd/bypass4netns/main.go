package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

var (
	socketFile  string
	pidFile     string
	logFilePath string
	readyFd     int
)

func main() {
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}

	flag.StringVar(&socketFile, "socket", filepath.Join(xdgRuntimeDir, bypass4netns.SocketName), "Socket file")
	flag.StringVar(&pidFile, "pid-file", "", "Pid file")
	flag.StringVar(&logFilePath, "log-file", "", "Output logs to file")
	flag.IntVar(&readyFd, "ready-fd", -1, "File descriptor to notify when ready")
	ignoredSubnets := flag.StringSlice("ignore", []string{"127.0.0.0/8"}, "Subnets to ignore in bypass4netns")
	fowardPorts := flag.StringArrayP("publish", "p", []string{}, "Publish a container's port(s) to the host")
	debug := flag.Bool("debug", false, "Enable debug mode")

	// Parse arguments
	flag.Parse()
	if flag.NArg() > 0 {
		flag.PrintDefaults()
		logrus.Fatal("Invalid command")
	}

	if *debug {
		logrus.Info("Debug mode enabled")
		logrus.SetLevel(logrus.DebugLevel)
	} else {
		logrus.SetLevel(logrus.InfoLevel)
	}

	if err := os.Remove(socketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logrus.Fatalf("Cannot cleanup socket file: %v", err)
	}

	if pidFile != "" {
		pid := fmt.Sprintf("%d", os.Getpid())
		if err := os.WriteFile(pidFile, []byte(pid), 0o644); err != nil {
			logrus.Fatalf("Cannot write pid file: %v", err)
		}
		logrus.Infof("PidFilePath: %s", pidFile)
	}

	if logFilePath != "" {
		logFile, err := os.Create(logFilePath)
		if err != nil {
			logrus.Fatalf("Cannnot write log file %s : %v", logFilePath, err)
		}
		defer logFile.Close()
		logrus.SetOutput(io.MultiWriter(os.Stderr, logFile))
		logrus.Infof("LogFilePath: %s", logFilePath)
	}

	logrus.Infof("SocketPath: %s", socketFile)
	handler := bypass4netns.NewHandler(socketFile)

	subnets := []net.IPNet{}
	for _, subnetStr := range *ignoredSubnets {
		_, subnet, err := net.ParseCIDR(subnetStr)
		if err != nil {
			logrus.Fatalf("%s is not CIDR format", subnetStr)
		}
		subnets = append(subnets, *subnet)
		logrus.Infof("%s is added to ignore", subnet)
	}
	handler.SetIgnoredSubnets(subnets)

	for _, forwardPortStr := range *fowardPorts {
		ports := strings.Split(forwardPortStr, ":")
		if len(ports) != 2 {
			logrus.Fatalf("invalid publish port format: '%s'", forwardPortStr)
		}
		hostPort, err := strconv.Atoi(ports[0])
		if err != nil {
			logrus.Fatalf("not interger %s in '%s'", ports[0], forwardPortStr)
		}
		childPort, err := strconv.Atoi(ports[1])
		if err != nil {
			logrus.Fatalf("not interger %s in '%s'", ports[1], forwardPortStr)
		}
		portMap := bypass4netns.ForwardPortMapping{
			HostPort:  hostPort,
			ChildPort: childPort,
		}
		err = handler.SetForwardingPort(portMap)
		if err != nil {
			logrus.Fatalf("failed to set fowardind port '%s' : %s", forwardPortStr, err)
		}
		logrus.Infof("fowarding port %s (host=%d container=%d) is added", forwardPortStr, hostPort, childPort)
	}

	if readyFd >= 0 {
		err := handler.SetReadyFd(readyFd)
		if err != nil {
			logrus.Fatalf("failed to set readyFd: %s", err)
		}
	}

	handler.StartHandle()
}
