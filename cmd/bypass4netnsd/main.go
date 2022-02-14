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
	"github.com/rootless-containers/bypass4netns/pkg/api/daemon/router"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netnsd"
	pkgversion "github.com/rootless-containers/bypass4netns/pkg/version"
	"github.com/sirupsen/logrus"
	flag "github.com/spf13/pflag"
)

var (
	socketFile  string
	pidFile     string
	logFilePath string
	b4nnPath    string
)

func main() {
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
	flag.StringVar(&pidFile, "pid-file", "", "Pid file")
	flag.StringVar(&logFilePath, "log-file", "", "Output logs to file")
	flag.StringVar(&b4nnPath, "b4nn-executable", defaultB4nnPath, "Path to bypass4netns executable")
	debug := flag.Bool("debug", false, "Enable debug mode")
	version := flag.Bool("version", false, "Show version")

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

	if err := os.Remove(socketFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		logrus.Fatalf("Cannot cleanup socket file: %v", err)
	}
	logrus.Infof("SocketPath: %s", socketFile)

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

	err = listenServeAPI(socketFile, &router.Backend{
		BypassDriver: bypass4netnsd.NewDriver(b4nnPath),
	})
	if err != nil {
		logrus.Fatalf("failed to serve API: %s", err)
	}
}

func listenServeAPI(socketPath string, backend *router.Backend) error {
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
	logrus.Infof("Starting to serve on %s", socketPath)
	srv.Serve(l)

	return nil
}
