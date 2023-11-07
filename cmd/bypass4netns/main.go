package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/nsagent"
	"github.com/rootless-containers/bypass4netns/pkg/oci"
	pkgversion "github.com/rootless-containers/bypass4netns/pkg/version"
	seccomp "github.com/seccomp/libseccomp-golang"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"golang.org/x/sys/unix"
)

var (
	socketFile    string
	comSocketFile string
	pidFile       string
	logFilePath   string
	readyFd       int
	exitFd        int
)

func main() {
	unix.Umask(0o077) // https://github.com/golang/go/issues/11822#issuecomment-123850227
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		panic("$XDG_RUNTIME_DIR needs to be set")
	}

	flag.StringVar(&socketFile, "socket", filepath.Join(xdgRuntimeDir, oci.SocketName), "Socket file")
	flag.StringVar(&comSocketFile, "com-socket", filepath.Join(xdgRuntimeDir, "bypass4netnsd-com.sock"), "Socket file for communication with bypass4netns")
	flag.StringVar(&pidFile, "pid-file", "", "Pid file")
	flag.StringVar(&logFilePath, "log-file", "", "Output logs to file")
	flag.IntVar(&readyFd, "ready-fd", -1, "File descriptor to notify when ready")
	flag.IntVar(&exitFd, "exit-fd", -1, "File descriptor for terminating bypass4netns")
	ignoredSubnets := flag.StringSlice("ignore", []string{"127.0.0.0/8"}, "Subnets to ignore in bypass4netns. Can be also set to \"auto\".")
	fowardPorts := flag.StringArrayP("publish", "p", []string{}, "Publish a container's port(s) to the host")
	debug := flag.Bool("debug", false, "Enable debug mode")
	version := flag.Bool("version", false, "Show version")
	help := flag.Bool("help", false, "Show help")
	nsagentFlag := flag.Bool("nsagent", false, "(An internal flag. Do not use manually.)") // TODO: hide

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

	if *version {
		fmt.Printf("bypass4netns version %s\n", strings.TrimPrefix(pkgversion.Version, "v"))
		major, minor, micro := seccomp.GetLibraryVersion()
		fmt.Printf("libseccomp: %d.%d.%d\n", major, minor, micro)
		os.Exit(0)
	}

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if *nsagentFlag {
		if err := nsagent.Main(); err != nil {
			logrus.Fatal(err)
		}
		os.Exit(0)
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

	handler := bypass4netns.NewHandler(socketFile, comSocketFile)

	subnets := []net.IPNet{}
	var subnetsAuto bool
	for _, subnetStr := range *ignoredSubnets {
		switch subnetStr {
		case "auto":
			if subnetsAuto {
				logrus.Warn("--ignore=\"auto\" appeared multiple times")
			}
			subnetsAuto = true
			logrus.Info("Enabling auto-update for --ignore")
		default:
			_, subnet, err := net.ParseCIDR(subnetStr)
			if err != nil {
				logrus.Fatalf("%s is not CIDR format", subnetStr)
			}
			subnets = append(subnets, *subnet)
			logrus.Infof("%s is added to ignore", subnet)
		}
	}
	handler.SetIgnoredSubnets(subnets, subnetsAuto)

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

	if exitFd >= 0 {
		exitFile := os.NewFile(uintptr(exitFd), "exit-fd")
		if exitFile == nil {
			logrus.Fatalf("invalid exit-fd %d", exitFd)
		}
		defer exitFile.Close()
		go func() {
			if _, err := io.ReadAll(exitFile); err != nil {
				logrus.Fatalf("Failed to wait for exit-fd %d to be closed: %v", exitFd, err)
			}
			pid := os.Getpid()
			logrus.Infof("The exit-fd was closed, sending SIGTERM to the process itself (PID %d)", pid)
			if err := unix.Kill(pid, unix.SIGTERM); err != nil {
				logrus.Fatalf("Failed to kill(%d, SIGTERM)", pid)
			}
		}()
	}

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, unix.SIGTERM, unix.SIGINT) // SIGHUP is propagated to nsagents for reloading
		sig := <-sigCh
		logrus.Infof("Received signal %v, exiting...", sig)
		logrus.Infof("Removing socket %q", socketFile)
		if err := os.RemoveAll(socketFile); err != nil {
			logrus.Warnf("Failed to remove socket %q", socketFile)
		}
		if pidFile != "" {
			logrus.Infof("Removing pid file %q", pidFile)
			if err := os.RemoveAll(pidFile); err != nil {
				logrus.Warnf("Failed to remove pid file %q", pidFile)
			}
		}
		// The log file is not removed here
		os.Exit(0)
	}()

	handler.StartHandle()
}
