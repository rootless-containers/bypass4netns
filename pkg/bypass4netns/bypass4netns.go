package bypass4netns

// This code is copied from 'runc(https://github.com/opencontainers/runc/blob/v1.1.0/contrib/cmd/seccompagent/seccompagent.go)'
// The code is licensed under Apache-2.0 License

import (
	"bytes"
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rootless-containers/bypass4netns/pkg/api/com"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/iproute2"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/nonbypassable"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/tracer"
	"github.com/rootless-containers/bypass4netns/pkg/util"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/sirupsen/logrus"
	clientv3 "go.etcd.io/etcd/client/v3"
	"golang.org/x/sys/unix"
)

const ETCD_MULTINODE_PREFIX = "bypass4netns/multinode/"

func closeStateFds(recvFds []int) {
	for i := range recvFds {
		unix.Close(i)
	}
}

// parseStateFds returns the seccomp-fd and closes the rest of the fds in recvFds.
// In case of error, no fd is closed.
// StateFds is assumed to be formatted as specs.ContainerProcessState.Fds and
// recvFds the corresponding list of received fds in the same SCM_RIGHT message.
func parseStateFds(stateFds []string, recvFds []int) (uintptr, error) {
	// Let's find the index in stateFds of the seccomp-fd.
	idx := -1
	err := false

	for i, name := range stateFds {
		if name == specs.SeccompFdName && idx == -1 {
			idx = i
			continue
		}

		// We found the seccompFdName twice. Error out!
		if name == specs.SeccompFdName && idx != -1 {
			err = true
		}
	}

	if idx == -1 || err {
		return 0, errors.New("seccomp fd not found or malformed containerProcessState.Fds")
	}

	if idx >= len(recvFds) || idx < 0 {
		return 0, errors.New("seccomp fd index out of range")
	}

	fd := uintptr(recvFds[idx])

	for i := range recvFds {
		if i == idx {
			continue
		}

		unix.Close(recvFds[i])
	}

	return fd, nil
}

// readProcMem read data from memory of specified pid process at the spcified offset.
func (h *notifHandler) readProcMem(pid int, offset uint64, len uint64) ([]byte, error) {
	buffer := make([]byte, len) // PATH_MAX

	memfd, err := h.openMem(pid)
	if err != nil {
		return nil, err
	}

	size, err := unix.Pread(memfd, buffer, int64(offset))
	if err != nil {
		return nil, err
	}

	return buffer[:size], nil
}

// writeProcMem writes data to memory of specified pid process at the specified offset.
func (h *notifHandler) writeProcMem(pid int, offset uint64, buf []byte) error {
	memfd, err := h.openMem(pid)
	if err != nil {
		return err
	}

	size, err := unix.Pwrite(memfd, buf, int64(offset))
	if err != nil {
		return err
	}
	if len(buf) != size {
		return fmt.Errorf("data is not written successfully. expected size=%d actual size=%d", len(buf), size)
	}

	return nil
}

func (h *notifHandler) openMem(pid int) (int, error) {
	if memfd, ok := h.memfds[pid]; ok {
		return memfd, nil
	}
	memfd, err := unix.Open(fmt.Sprintf("/proc/%d/mem", pid), unix.O_RDWR, 0o777)
	if err != nil {
		logrus.WithField("pid", pid).Warn("failed to open mem due to permission error. retrying with agent.")
		newMemfd, err := openMemWithNSEnter(pid)
		if err != nil {
			return 0, fmt.Errorf("failed to open mem with agent (pid=%d)", pid)
		}
		logrus.WithField("pid", pid).Info("succeeded to open mem with agent. continue to process")
		memfd = newMemfd
	}
	h.memfds[pid] = memfd

	return memfd, nil
}

func openMemWithNSEnter(pid int) (int, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return 0, err
	}

	// configure timeout
	timeout := &syscall.Timeval{
		Sec:  0,
		Usec: 500 * 1000,
	}
	err = syscall.SetsockoptTimeval(fds[0], syscall.SOL_SOCKET, syscall.SO_RCVTIMEO, timeout)
	if err != nil {
		return 0, fmt.Errorf("failed to set receive timeout")
	}
	err = syscall.SetsockoptTimeval(fds[1], syscall.SOL_SOCKET, syscall.SO_SNDTIMEO, timeout)
	if err != nil {
		return 0, fmt.Errorf("failed to set send timeout")
	}

	fd1File := os.NewFile(uintptr(fds[0]), "")
	defer fd1File.Close()
	fd1Conn, err := net.FileConn(fd1File)
	if err != nil {
		return 0, err
	}
	_ = fd1Conn

	selfExe, err := os.Executable()
	if err != nil {
		return 0, err
	}
	nsenter, err := exec.LookPath("nsenter")
	if err != nil {
		return 0, err
	}
	nsenterFlags := []string{
		"-t", strconv.Itoa(int(pid)),
		"-F",
	}
	selfPid := os.Getpid()
	ok, err := util.SameUserNS(int(pid), selfPid)
	if err != nil {
		return 0, fmt.Errorf("failed to check sameUserNS(%d, %d)", pid, selfPid)
	}
	if !ok {
		nsenterFlags = append(nsenterFlags, "-U", "--preserve-credentials")
	}
	nsenterFlags = append(nsenterFlags, "--", selfExe, fmt.Sprintf("--mem-nsenter-pid=%d", pid))
	cmd := exec.CommandContext(gocontext.TODO(), nsenter, nsenterFlags...)
	cmd.ExtraFiles = []*os.File{os.NewFile(uintptr(fds[1]), "")}
	stdout := bytes.Buffer{}
	cmd.Stdout = &stdout
	err = cmd.Start()
	if err != nil {
		return 0, fmt.Errorf("failed to exec mem open agent %q", err)
	}
	memfd, recvMsgs, err := util.RecvMsg(fd1Conn)
	if err != nil {
		logrus.Infof("stdout=%q", stdout.String())
		return 0, fmt.Errorf("failed to receive message")
	}
	logrus.Debugf("recvMsgs=%s", string(recvMsgs))
	err = cmd.Wait()
	if err != nil {
		return 0, err
	}

	return memfd, nil
}

func OpenMemWithNSEnterAgent(pid uint32) error {
	// fd 3 should be passed socket pair
	fdFile := os.NewFile(uintptr(3), "")
	defer fdFile.Close()
	fdConn, err := net.FileConn(fdFile)
	if err != nil {
		logrus.WithError(err).Fatal("failed to open conn")
	}
	memPath := fmt.Sprintf("/proc/%d/mem", pid)
	memfd, err := unix.Open(memPath, unix.O_RDWR, 0o777)
	if err != nil {
		logrus.WithError(err).Fatalf("failed to open %s", memPath)
	}
	err = util.SendMsg(fdConn, memfd, []byte(fmt.Sprintf("opened %s", memPath)))
	if err != nil {
		logrus.WithError(err).Fatal("failed to send message")
	}
	return nil
}

func handleNewMessage(sockfd int) (uintptr, *specs.ContainerProcessState, error) {
	const maxNameLen = 4096
	stateBuf := make([]byte, maxNameLen)
	oobSpace := unix.CmsgSpace(4)
	oob := make([]byte, oobSpace)

	n, oobn, _, _, err := unix.Recvmsg(sockfd, stateBuf, oob, 0)
	if err != nil {
		return 0, nil, err
	}
	if n >= maxNameLen || oobn != oobSpace {
		return 0, nil, fmt.Errorf("recvfd: incorrect number of bytes read (n=%d oobn=%d)", n, oobn)
	}

	// Truncate.
	stateBuf = stateBuf[:n]
	oob = oob[:oobn]

	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, nil, err
	}
	if len(scms) != 1 {
		return 0, nil, fmt.Errorf("recvfd: number of SCMs is not 1: %d", len(scms))
	}
	scm := scms[0]

	fds, err := unix.ParseUnixRights(&scm)
	if err != nil {
		return 0, nil, err
	}

	containerProcessState := &specs.ContainerProcessState{}
	err = json.Unmarshal(stateBuf, containerProcessState)
	if err != nil {
		closeStateFds(fds)
		return 0, nil, fmt.Errorf("cannot parse OCI state: %w", err)
	}

	fd, err := parseStateFds(containerProcessState.Fds, fds)
	if err != nil {
		closeStateFds(fds)
		return 0, nil, err
	}

	return fd, containerProcessState, nil
}

type context struct {
	notifFd libseccomp.ScmpFd
	req     *libseccomp.ScmpNotifReq
	resp    *libseccomp.ScmpNotifResp
}

func (h *notifHandler) getPidFdInfo(pid int) (*pidInfo, error) {
	// retrieve pidfd from cache
	if pidfd, ok := h.pidInfos[pid]; ok {
		return &pidfd, nil
	}

	targetPidfd, err := unix.PidfdOpen(int(pid), 0)
	if err == nil {
		info := pidInfo{
			pidType: PROCESS,
			pidfd:   targetPidfd,
			tgid:    pid, // process's pid is equal to its tgid
		}
		h.pidInfos[pid] = info
		return &info, nil
	}

	// pid can be thread and pidfd_open fails with thread's pid.
	// retrieve process's pid (tgid) from /proc/<pid>/status and retry to get pidfd with the tgid.
	logrus.Warnf("pidfd Open failed: pid=%d err=%q, this pid maybe thread and retrying with tgid", pid, err)
	st, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil, fmt.Errorf("failed to read %d's status err=%q", pid, err)
	}

	nextTgid := -1
	for _, s := range strings.Split(string(st), "\n") {
		if strings.Contains(s, "Tgid") {
			tgids := strings.Split(s, "\t")
			if len(tgids) < 2 {
				return nil, fmt.Errorf("unexpected /proc/%d/status len=%q status=%q", pid, len(tgids), string(st))
			}
			tgid, err := strconv.Atoi(tgids[1])
			if err != nil {
				return nil, fmt.Errorf("unexpected /proc/%d/status err=%q status=%q", pid, err, string(st))
			}
			nextTgid = tgid
		}
		if nextTgid > 0 {
			break
		}
	}
	if nextTgid < 0 {
		logrus.Errorf("cannot get Tgid from /proc/%d/status status=%q", pid, string(st))
	}
	targetPidfd, err = unix.PidfdOpen(nextTgid, 0)
	if err != nil {
		return nil, fmt.Errorf("pidfd Open failed with Tgid: pid=%d %s", nextTgid, err)
	}

	logrus.Infof("successfully got pidfd for pid=%d tgid=%d", pid, nextTgid)
	info := pidInfo{
		pidType: THREAD,
		pidfd:   targetPidfd,
		tgid:    nextTgid,
	}
	h.pidInfos[pid] = info
	return &info, nil
}

// getFdInProcess get the file descriptor in other process
func (h *notifHandler) getFdInProcess(pid, targetFd int) (int, error) {
	targetPidfd, err := h.getPidFdInfo(pid)
	if err != nil {
		return 0, fmt.Errorf("pidfd Open failed: %s", err)
	}

	fd, err := unix.PidfdGetfd(targetPidfd.pidfd, targetFd, 0)
	if err != nil {
		return 0, fmt.Errorf("pidfd GetFd failed: %s", err)
	}

	return fd, nil
}

// getSocketArgs retrieves socket(2) arguemnts from fd.
// return values are (sock_domain, sock_type, sock_protocol, error)
func getSocketArgs(sockfd int) (int, int, int, error) {
	logrus.Debugf("got sockfd=%v", sockfd)
	sock_domain, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getsockopt(SO_DOMAIN) failed: %s", err)
	}

	sock_type, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getsockopt(SO_TYPE) failed: %s", err)
	}

	sock_protocol, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("getsockopt(SO_PROTOCOL) failed: %s", err)
	}

	return sock_domain, sock_type, sock_protocol, nil
}

func (h *notifHandler) readSockaddrFromProcess(pid int, offset uint64, addrlen uint64) (*sockaddr, error) {
	buf, err := h.readProcMem(pid, offset, addrlen)
	if err != nil {
		return nil, fmt.Errorf("failed readProcMem pid %v offset 0x%x: %s", pid, offset, err)
	}
	return newSockaddr(buf)
}

func (h *notifHandler) registerSocket(pid int, sockfd int, syscallName string) (*socketStatus, error) {
	logger := logrus.WithFields(logrus.Fields{"pid": pid, "sockfd": sockfd, "syscall": syscallName})
	proc, ok := h.processes[pid]
	if !ok {
		proc = newProcessStatus()
		h.processes[pid] = proc
		logger.Debug("process is registered")
	}

	sock, ok := proc.sockets[sockfd]
	if ok {
		logger.Warn("socket is already registered")
		return sock, nil
	}

	// If the pid is thread, its process can have corresponding socket
	procInfo, ok := h.pidInfos[int(pid)]
	if ok && procInfo.pidType == THREAD {
		return nil, fmt.Errorf("unexpected procInfo")
	}

	sockFdHost, err := h.getFdInProcess(int(pid), sockfd)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(sockFdHost)

	sockDomain, sockType, sockProtocol, err := getSocketArgs(sockFdHost)
	sock = newSocketStatus(pid, sockfd, sockDomain, sockType, sockProtocol)
	if err != nil {
		// non-socket fd is not bypassable
		sock.state = NotBypassable
		logger.Debugf("failed to get socket args err=%q", err)
	} else {
		if sockDomain != syscall.AF_INET && sockDomain != syscall.AF_INET6 {
			// non IP sockets are not handled.
			sock.state = NotBypassable
			logger.Debugf("socket domain=0x%x", sockDomain)
		} else if sockType != syscall.SOCK_STREAM {
			// only accepting TCP socket
			sock.state = NotBypassable
			logger.Debugf("socket type=0x%x", sockType)
		} else {
			// only newly created socket is allowed.
			_, err := syscall.Getpeername(sockFdHost)
			if err == nil {
				logger.Infof("socket is already connected. socket is created via accept or forked")
				sock.state = NotBypassable
			}
		}
	}

	proc.sockets[sockfd] = sock
	if sock.state == NotBypassable {
		logger.Debugf("socket is registered (state=%s)", sock.state)
	} else {
		logger.Infof("socket is registered (state=%s)", sock.state)
	}

	return sock, nil
}

func (h *notifHandler) getSocket(pid int, sockfd int) *socketStatus {
	proc, ok := h.processes[pid]
	if !ok {
		return nil
	}
	sock := proc.sockets[sockfd]
	return sock
}

func (h *notifHandler) removeSocket(pid int, sockfd int) {
	defer logrus.WithFields(logrus.Fields{"pid": pid, "sockfd": sockfd}).Debugf("socket is removed")
	proc, ok := h.processes[pid]
	if !ok {
		return
	}
	delete(proc.sockets, sockfd)
}

// handleReq handles seccomp notif requests and configures responses.
func (h *notifHandler) handleReq(ctx *context) {
	syscallName, err := ctx.req.Data.Syscall.GetName()
	if err != nil {
		logrus.Errorf("Error decoding syscall %v(): %s", ctx.req.Data.Syscall, err)
		// TODO: error handle
		return
	}
	logrus.Tracef("Received syscall %q, pid %v, arch %q, args %+v", syscallName, ctx.req.Pid, ctx.req.Data.Arch, ctx.req.Data.Args)

	ctx.resp.Flags |= SeccompUserNotifFlagContinue

	// ensure pid is registered in notifHandler.pidInfos
	pidInfo, err := h.getPidFdInfo(int(ctx.req.Pid))
	if err != nil {
		logrus.Errorf("failed to get pidfd err=%q", err)
		return
	}

	// threads shares file descriptors in the same process space.
	// so use tgid as pid to process socket file descriptors
	pid := pidInfo.tgid
	if pidInfo.pidType == THREAD {
		logrus.Debugf("pid %d is thread. use process's tgid %d as pid", ctx.req.Pid, pid)
	}

	// cleanup sockets when the process exit.
	if syscallName == "_exit" || syscallName == "exit_group" {
		if pidInfo, ok := h.pidInfos[int(ctx.req.Pid)]; ok {
			syscall.Close(int(pidInfo.pidfd))
			delete(h.pidInfos, int(ctx.req.Pid))
		}
		if pidInfo.pidType == THREAD {
			logrus.WithFields(logrus.Fields{"pid": ctx.req.Pid, "tgid": pid}).Infof("thread is removed")
		}

		if pidInfo.pidType == PROCESS {
			delete(h.processes, pid)
			if memfd, ok := h.memfds[pid]; ok {
				syscall.Close(memfd)
				delete(h.memfds, pid)
			}
			logrus.WithFields(logrus.Fields{"pid": pid}).Infof("process is removed")
		}
		return
	}

	sockfd := int(ctx.req.Data.Args[0])
	// remove socket when closed
	if syscallName == "close" {
		h.removeSocket(pid, sockfd)
		return
	}

	sock := h.getSocket(pid, sockfd)
	if sock == nil {
		sock, err = h.registerSocket(pid, sockfd, syscallName)
		if err != nil {
			logrus.Errorf("failed to register socket pid %d sockfd %d: %s", pid, sockfd, err)
			return
		}
	}

	switch sock.state {
	case NotBypassable:
		// sometimes close(2) is not called for the fd.
		// To handle such condition, re-register fd when connect is called for not bypassable fd.
		if syscallName == "connect" {
			h.removeSocket(pid, sockfd)
			sock, err = h.registerSocket(pid, sockfd, syscallName)
			if err != nil {
				logrus.Errorf("failed to re-register socket pid %d sockfd %d: %s", pid, sockfd, err)
				return
			}
		}
		if sock.state != NotBypassed {
			return
		}

		// when sock.state == NotBypassed, continue
	case Bypassed:
		if syscallName == "getpeername" {
			sock.handleSysGetpeername(h, ctx)
		}
		return
	default:
	}

	switch syscallName {
	case "bind":
		sock.handleSysBind(pid, h, ctx)
	case "connect":
		sock.handleSysConnect(h, ctx)
	case "setsockopt":
		sock.handleSysSetsockopt(pid, h, ctx)
	case "fcntl":
		sock.handleSysFcntl(ctx)
	case "getpeername":
		// already handled
	default:
		logrus.Errorf("Unknown syscall %q", syscallName)
		// TODO: error handle
		return
	}

}

// notifHandler handles seccomp notifications and response to them.
func (h *notifHandler) handle() {
	defer unix.Close(int(h.fd))
	if h.nonBypassableAutoUpdate {
		go func() {
			if nbErr := h.nonBypassable.WatchNS(gocontext.TODO(), h.state.Pid); nbErr != nil {
				logrus.WithError(nbErr).Fatalf("failed to watch NS (PID=%d)", h.state.Pid)
			}
		}()
	}

	for {
		req, err := libseccomp.NotifReceive(h.fd)
		if err != nil {
			logrus.Errorf("Error in NotifReceive(): %s", err)
			continue
		}

		ctx := context{
			notifFd: h.fd,
			req:     req,
			resp: &libseccomp.ScmpNotifResp{
				ID:    req.ID,
				Error: 0,
				Val:   0,
				Flags: libseccomp.NotifRespFlagContinue,
			},
		}

		// TOCTOU check
		if err := libseccomp.NotifIDValid(h.fd, req.ID); err != nil {
			logrus.Errorf("TOCTOU check failed: req.ID is no longer valid: %s", err)
			continue
		}

		h.handleReq(&ctx)

		if err = libseccomp.NotifRespond(h.fd, ctx.resp); err != nil {
			logrus.Errorf("Error in notification response: %s", err)
			continue
		}
	}
}

type ForwardPortMapping struct {
	HostPort  int
	ChildPort int
}

type Handler struct {
	socketPath               string
	comSocketPath            string
	tracerAgentLogPath       string
	ignoredSubnets           []net.IPNet
	ignoredSubnetsAutoUpdate bool
	readyFd                  int

	// key is child port
	forwardingPorts map[int]ForwardPortMapping
}

// NewHandler creates new seccomp notif handler
func NewHandler(socketPath, comSocketPath, tracerAgentLogPath string) *Handler {
	handler := Handler{
		socketPath:         socketPath,
		comSocketPath:      comSocketPath,
		tracerAgentLogPath: tracerAgentLogPath,
		ignoredSubnets:     []net.IPNet{},
		forwardingPorts:    map[int]ForwardPortMapping{},
		readyFd:            -1,
	}

	return &handler
}

// SetIgnoreSubnets configures subnets to ignore in bypass4netns.
func (h *Handler) SetIgnoredSubnets(subnets []net.IPNet, autoUpdate bool) {
	h.ignoredSubnets = subnets
	h.ignoredSubnetsAutoUpdate = autoUpdate
}

// SetForwardingPort checks and configures port forwarding
func (h *Handler) SetForwardingPort(mapping ForwardPortMapping) error {
	for _, fwd := range h.forwardingPorts {
		if fwd.HostPort == mapping.HostPort {
			return fmt.Errorf("host port %d is already forwarded", fwd.HostPort)
		}
		if fwd.ChildPort == mapping.ChildPort {
			return fmt.Errorf("container port %d is already forwarded", fwd.ChildPort)
		}
	}

	h.forwardingPorts[mapping.ChildPort] = mapping
	return nil
}

// SetReadyFd configure ready notification file descriptor
func (h *Handler) SetReadyFd(fd int) error {
	if fd < 0 {
		return fmt.Errorf("ready-fd must be a non-negative integer")
	}

	h.readyFd = fd
	return nil
}

type MultinodeConfig struct {
	Enable           bool
	EtcdAddress      string
	HostAddress      string
	etcdClientConfig clientv3.Config
	etcdClient       *clientv3.Client
}

type C2CConnectionHandleConfig struct {
	Enable       bool
	TracerEnable bool
}

type notifHandler struct {
	fd                      libseccomp.ScmpFd
	state                   *specs.ContainerProcessState
	nonBypassable           *nonbypassable.NonBypassable
	nonBypassableAutoUpdate bool

	// key is child port
	forwardingPorts map[int]ForwardPortMapping

	// key is pid
	processes map[int]*processStatus

	// key is destination address e.g. "192.168.1.1:1000"
	containerInterfaces map[string]containerInterface
	c2cConnections      *C2CConnectionHandleConfig
	multinode           *MultinodeConfig

	// cache /proc/<pid>/mem's fd to reduce latency. key is pid, value is fd
	memfds map[int]int

	// cache pidfd to reduce latency. key is pid.
	pidInfos map[int]pidInfo
}

type containerInterface struct {
	containerID     string
	hostPort        int
	lastCheckedUnix int64
}

type pidInfoPidType int

const (
	PROCESS pidInfoPidType = iota
	THREAD
)

type pidInfo struct {
	pidType pidInfoPidType
	pidfd   int
	tgid    int
}

func (h *Handler) newNotifHandler(fd uintptr, state *specs.ContainerProcessState) *notifHandler {
	notifHandler := notifHandler{
		fd:              libseccomp.ScmpFd(fd),
		state:           state,
		forwardingPorts: map[int]ForwardPortMapping{},
		processes:       map[int]*processStatus{},
		memfds:          map[int]int{},
		pidInfos:        map[int]pidInfo{},
	}
	notifHandler.nonBypassable = nonbypassable.New(h.ignoredSubnets)
	notifHandler.nonBypassableAutoUpdate = h.ignoredSubnetsAutoUpdate

	// Deep copy of map
	for key, value := range h.forwardingPorts {
		notifHandler.forwardingPorts[key] = value
	}

	return &notifHandler
}

// StartHandle starts seccomp notif handler
func (h *Handler) StartHandle(c2cConfig *C2CConnectionHandleConfig, multinodeConfig *MultinodeConfig) {
	logrus.Info("Waiting for seccomp file descriptors")
	l, err := net.Listen("unix", h.socketPath)
	if err != nil {
		logrus.Fatalf("Cannot listen: %v", err)
	}
	defer l.Close()

	if h.readyFd >= 0 {
		logrus.Infof("notify ready fd=%d", h.readyFd)
		_, err = syscall.Write(h.readyFd, []byte{1})
		if err != nil {
			logrus.Fatalf("failed to notify fd=%d", h.readyFd)
		}
		syscall.Close(h.readyFd)
	}

	// prepare tracer agent
	var tracerAgent *tracer.Tracer = nil

	for {
		conn, err := l.Accept()
		logrus.Info("accept connection")
		if err != nil {
			logrus.Errorf("Cannot accept connection: %s", err)
			continue
		}
		socket, err := conn.(*net.UnixConn).File()
		conn.Close()
		if err != nil {
			logrus.Errorf("Cannot get socket: %v", err)
			continue
		}
		newFd, state, err := handleNewMessage(int(socket.Fd()))
		socket.Close()
		if err != nil {
			logrus.Errorf("Error receiving seccomp file descriptor: %v", err)
			continue
		}

		logrus.Infof("Received new seccomp fd: %v", newFd)
		notifHandler := h.newNotifHandler(newFd, state)
		notifHandler.c2cConnections = c2cConfig
		notifHandler.multinode = multinodeConfig
		if notifHandler.multinode.Enable {
			notifHandler.multinode.etcdClientConfig = clientv3.Config{
				Endpoints: []string{notifHandler.multinode.EtcdAddress},
			}
			notifHandler.multinode.etcdClient, err = clientv3.New(notifHandler.multinode.etcdClientConfig)
			if err != nil {
				logrus.WithError(err).Fatal("failed to create etcd client")
			}
		}

		// not to run multiple tracerAgent.
		// TODO: prepare only one tracerAgent in Handler
		if c2cConfig.TracerEnable && !multinodeConfig.Enable && tracerAgent == nil {
			tracerAgent = tracer.NewTracer(h.tracerAgentLogPath)
			err = tracerAgent.StartTracer(gocontext.TODO(), state.Pid)
			if err != nil {
				logrus.WithError(err).Fatalf("failed to start tracer")
			}
			fwdPorts := []int{}
			for _, v := range notifHandler.forwardingPorts {
				fwdPorts = append(fwdPorts, v.ChildPort)
			}
			err = tracerAgent.RegisterForwardPorts(fwdPorts)
			if err != nil {
				logrus.WithError(err).Fatalf("failed to register port")
			}
			logrus.WithField("fwdPorts", fwdPorts).Info("registered ports to tracer agent")

			// check tracer agent is ready
			for _, v := range fwdPorts {
				dst := fmt.Sprintf("127.0.0.1:%d", v)
				addr, err := tracerAgent.ConnectToAddress([]string{dst})
				if err != nil {
					logrus.WithError(err).Warnf("failed to connect to %s", dst)
					continue
				}
				if len(addr) != 1 || addr[0] != dst {
					logrus.Fatalf("failed to connect to %s", dst)
					continue
				}
				logrus.Debugf("successfully connected to %s", dst)
			}
			logrus.Infof("tracer is ready")
		} else {
			logrus.Infof("tracer is disabled")
		}

		// TODO: these goroutines shoud be launched only once.
		ready := make(chan bool, 10)
		if notifHandler.multinode.Enable {
			go notifHandler.startBackgroundMultinodeTask(ready)
		} else if notifHandler.c2cConnections.Enable {
			go notifHandler.startBackgroundC2CConnectionHandleTask(ready, h.comSocketPath, tracerAgent)
		} else {
			ready <- true
		}

		// wait for background tasks becoming ready
		<-ready
		logrus.Info("background task is ready. start to handle")
		go notifHandler.handle()
	}
}

func (h *notifHandler) startBackgroundC2CConnectionHandleTask(ready chan bool, comSocketPath string, tracerAgent *tracer.Tracer) {
	initDone := false
	logrus.Info("Started bypass4netns background task")
	comClient, err := com.NewComClient(comSocketPath)
	if err != nil {
		logrus.Fatalf("failed to create ComClient: %q", err)
	}
	err = comClient.Ping(gocontext.TODO())
	if err != nil {
		logrus.Fatalf("failed to connect to bypass4netnsd: %q", err)
	}
	logrus.Infof("Successfully connected to bypass4netnsd")
	ifLastUpdateUnix := int64(0)
	for {
		if ifLastUpdateUnix+10 < time.Now().Unix() {
			addrs, err := iproute2.GetAddressesInNetNS(gocontext.TODO(), h.state.Pid)
			if err != nil {
				logrus.WithError(err).Errorf("failed to get addresses")
				return
			}
			ifs, err := iproute2AddressesToComInterfaces(addrs)
			if err != nil {
				logrus.WithError(err).Errorf("failed to convert addresses")
				return
			}
			containerIfs := &com.ContainerInterfaces{
				ContainerID:     h.state.State.ID,
				Interfaces:      ifs,
				ForwardingPorts: map[int]int{},
			}
			for _, v := range h.forwardingPorts {
				containerIfs.ForwardingPorts[v.ChildPort] = v.HostPort
			}
			logrus.Debugf("Interfaces = %v", containerIfs)
			_, err = comClient.PostInterface(gocontext.TODO(), containerIfs)
			if err != nil {
				logrus.WithError(err).Errorf("failed to post interfaces")
			} else {
				logrus.Infof("successfully posted updated interfaces")
				ifLastUpdateUnix = time.Now().Unix()
			}
		}
		containerInterfaces, err := comClient.ListInterfaces(gocontext.TODO())
		if err != nil {
			logrus.WithError(err).Warn("failed to list container interfaces")
		}

		containerIf := map[string]containerInterface{}
		for _, cont := range containerInterfaces {
			for contPort, hostPort := range cont.ForwardingPorts {
				for _, intf := range cont.Interfaces {
					if intf.IsLoopback {
						continue
					}
					for _, addr := range intf.Addresses {
						// ignore ipv6 address
						if addr.IP.To4() == nil {
							continue
						}
						dstAddr := fmt.Sprintf("%s:%d", addr.IP, contPort)
						contIf, ok := h.containerInterfaces[dstAddr]
						if ok && contIf.lastCheckedUnix+10 > time.Now().Unix() {
							containerIf[dstAddr] = contIf
							continue
						}
						if h.c2cConnections.TracerEnable {
							addrRes, err := tracerAgent.ConnectToAddress([]string{dstAddr})
							if err != nil {
								logrus.WithError(err).Debugf("failed to connect to %s", dstAddr)
								continue
							}
							if len(addrRes) != 1 || addrRes[0] != dstAddr {
								logrus.Debugf("failed to connect to %s", dstAddr)
								continue
							}
							logrus.Debugf("successfully connected to %s", dstAddr)
						}
						containerIf[dstAddr] = containerInterface{
							containerID:     cont.ContainerID,
							hostPort:        hostPort,
							lastCheckedUnix: time.Now().Unix(),
						}
						logrus.Infof("%s -> 127.0.0.1:%d is registered", dstAddr, hostPort)
					}
				}
			}
		}
		h.containerInterfaces = containerIf

		// once the interfaces are registered, it is ready to handle connections
		if !initDone {
			initDone = true
			ready <- true
		}

		time.Sleep(1 * time.Second)
	}
}

func iproute2AddressesToComInterfaces(addrs iproute2.Addresses) ([]com.Interface, error) {
	comIntfs := []com.Interface{}
	for _, intf := range addrs {
		comIntf := com.Interface{
			Name:       intf.IfName,
			Addresses:  []net.IPNet{},
			IsLoopback: intf.LinkType == "loopback",
		}
		hwAddr, err := net.ParseMAC(intf.Address)
		if err != nil {
			return nil, fmt.Errorf("failed to parse HWAddress: %w", err)
		}
		comIntf.HWAddr = hwAddr
		for _, addr := range intf.AddrInfos {
			ip, ipNet, err := net.ParseCIDR(fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
			if err != nil {
				return nil, fmt.Errorf("failed to parse addr_info: %w", err)
			}
			ipNet.IP = ip
			comIntf.Addresses = append(comIntf.Addresses, *ipNet)
		}

		comIntfs = append(comIntfs, comIntf)
	}

	return comIntfs, nil
}

func (h *notifHandler) startBackgroundMultinodeTask(ready chan bool) {
	initDone := false
	ifLastUpdateUnix := int64(0)
	for {
		if ifLastUpdateUnix+10 < time.Now().Unix() {
			ifs, err := iproute2.GetAddressesInNetNS(gocontext.TODO(), h.state.Pid)
			if err != nil {
				logrus.WithError(err).Errorf("failed to get addresses")
				return
			}
			for _, intf := range ifs {
				// ignore non-ethernet interface
				if intf.LinkType != "ether" {
					continue
				}
				for _, addr := range intf.AddrInfos {
					// ignore non-IPv4 address
					if addr.Family != "inet" {
						continue
					}
					for _, v := range h.forwardingPorts {
						containerAddr := fmt.Sprintf("%s:%d", addr.Local, v.ChildPort)
						hostAddr := fmt.Sprintf("%s:%d", h.multinode.HostAddress, v.HostPort)
						// Remove entries with timeout
						// TODO: Remove related entries when exiting.
						ctx, cancel := gocontext.WithTimeout(gocontext.Background(), 2*time.Second)
						lease, err := h.multinode.etcdClient.Grant(ctx, 15)
						cancel()
						if err != nil {
							logrus.WithError(err).Errorf("failed to grant lease to register %s -> %s", containerAddr, hostAddr)
							continue
						}
						ctx, cancel = gocontext.WithTimeout(gocontext.Background(), 2*time.Second)
						_, err = h.multinode.etcdClient.Put(ctx, ETCD_MULTINODE_PREFIX+containerAddr, hostAddr,
							clientv3.WithLease(lease.ID))
						cancel()
						if err != nil {
							logrus.WithError(err).Errorf("failed to register %s -> %s", containerAddr, hostAddr)
						} else {
							logrus.Infof("Registered %s -> %s", containerAddr, hostAddr)
						}
					}
				}
			}
			ifLastUpdateUnix = time.Now().Unix()

			// once the interfaces are registered, it is ready to handle connections
			if !initDone {
				initDone = true
				ready <- true
			}
		}

		time.Sleep(1 * time.Second)
	}
}
