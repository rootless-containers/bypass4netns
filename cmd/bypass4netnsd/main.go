package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/mux"
	"github.com/rootless-containers/bypass4netns/pkg/api/com"
	"github.com/rootless-containers/bypass4netns/pkg/api/daemon/router"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netnsd"
	pkgversion "github.com/rootless-containers/bypass4netns/pkg/version"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
	"golang.org/x/sys/unix"
)

var (
	socketFile           string
	comSocketFile        string // socket for channel with bypass4netns
	pidFile              string
	logFilePath          string
	b4nnPath             string
	multinodeEtcdAddress string
	multinodeHostAddress string
)

func main() {
	unix.Umask(0o077) // https://github.com/golang/go/issues/11822#issuecomment-123850227
	xdgRuntimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if xdgRuntimeDir == "" {
		logrus.Fatalf("$XDG_RUNTIME_DIR needs to be set")
	}
	exePath, err := os.Executable()
	if err != nil {
		logrus.Fatalf("failed to get myself executable path: %s", err)
	}
	defaultB4nnPath := filepath.Join(filepath.Dir(exePath), "bypass4netns")

	flag.StringVar(&socketFile, "socket", filepath.Join(xdgRuntimeDir, "bypass4netnsd.sock"), "Socket file")
	flag.StringVar(&comSocketFile, "com-socket", filepath.Join(xdgRuntimeDir, "bypass4netnsd-com.sock"), "Socket file for communication with bypass4netns")
	flag.StringVar(&pidFile, "pid-file", "", "Pid file")
	flag.StringVar(&logFilePath, "log-file", "", "Output logs to file")
	flag.StringVar(&b4nnPath, "b4nn-executable", defaultB4nnPath, "Path to bypass4netns executable")
	flag.StringVar(&multinodeEtcdAddress, "multinode-etcd-address", "", "Etcd address for multinode communication")
	flag.StringVar(&multinodeHostAddress, "multinode-host-address", "", "Host address for multinode communication")
	tracerEnable := flag.Bool("tracer", false, "Enable connection tracer")
	multinodeEnable := flag.Bool("multinode", false, "Enable multinode communication")
	debug := flag.Bool("debug", false, "Enable debug mode")
	version := flag.Bool("version", false, "Show version")
	help := flag.Bool("help", false, "Show help")

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
		fmt.Printf("bypass4netnsd version %s\n", strings.TrimPrefix(pkgversion.Version, "v"))
		os.Exit(0)
	}

	if *help {
		flag.Usage()
		os.Exit(0)
	}

	if err := os.Remove(socketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logrus.Fatalf("Cannot cleanup socket file: %v", err)
	}
	logrus.Infof("SocketPath: %s", socketFile)

	if err := os.Remove(comSocketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logrus.Fatalf("Cannot cleanup communication socket file: %v", err)
	}
	logrus.Infof("CommunicationSocketPath: %s", comSocketFile)

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
		logrus.Infof("LogFilePath %s", logFilePath)
	}

	if _, err = os.Stat(b4nnPath); err != nil {
		logrus.Fatalf("bypass4netns executable not found %s", b4nnPath)
	}
	logrus.Infof("bypass4netns executable path: %s", b4nnPath)

	b4nsdDriver := bypass4netnsd.NewDriver(b4nnPath, comSocketFile)

	if *tracerEnable {
		logrus.Info("Connection tracer is enabled")
		b4nsdDriver.TracerEnable = *tracerEnable
	}

	if *multinodeEnable {
		if multinodeEtcdAddress == "" {
			logrus.Fatal("--multinode-etcd-address is not specified")
		}
		if multinodeHostAddress == "" {
			logrus.Fatal("--multinode-host-address is not specified")
		}
		b4nsdDriver.MultinodeEnable = *multinodeEnable
		b4nsdDriver.MultinodeEtcdAddress = multinodeEtcdAddress
		b4nsdDriver.MultinodeHostAddress = multinodeHostAddress
		logrus.WithFields(logrus.Fields{"etcdAddress": multinodeEtcdAddress, "hostAddress": multinodeHostAddress}).Info("Multinode communication is enabled.")
	}

	waitChan := make(chan bool)
	go func() {
		err = listenServeNerdctlAPI(socketFile, &router.Backend{
			BypassDriver: b4nsdDriver,
		})
		if err != nil {
			logrus.Fatalf("failed to serve nerdctl API: %q", err)
		}
		waitChan <- true
	}()

	go func() {
		err = listenServeBypass4netnsAPI(comSocketFile, &com.Backend{
			BypassDriver: b4nsdDriver,
		})
		if err != nil {
			logrus.Fatalf("failed to serve bypass4netns: %q", err)
		}
		waitChan <- true
	}()

	<-waitChan
	logrus.Fatalf("process exited")
}

func listenServeNerdctlAPI(socketPath string, backend *router.Backend) error {
	r := mux.NewRouter()
	router.AddRoutes(r, backend)
	srv := &http.Server{Handler: r}
	err := os.RemoveAll(socketPath)
	if err != nil {
		return err
	}
	l, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	logrus.Infof("Starting nerdctl API to serve on %s", socketPath)
	return srv.Serve(l)
}

func listenServeBypass4netnsAPI(sockPath string, backend *com.Backend) error {
	r := mux.NewRouter()
	com.AddRoutes(r, backend)
	srv := &http.Server{Handler: r}
	err := os.RemoveAll(sockPath)
	if err != nil {
		return err
	}
	l, err := net.Listen("unix", sockPath)
	if err != nil {
		return err
	}
	logrus.Infof("Starting bypass4netns API to serve on %s", sockPath)
	return srv.Serve(l)
}
