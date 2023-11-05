package bypass4netns

// This code is copied from 'runc(https://github.com/opencontainers/runc/blob/v1.1.0/contrib/cmd/seccompagent/seccompagent.go)'
// The code is licensed under Apache-2.0 License

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/oraoto/go-pidfd"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/nonbypassable"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

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
func readProcMem(pid uint32, offset uint64, len uint64) ([]byte, error) {
	buffer := make([]byte, len) // PATH_MAX

	memfd, err := unix.Open(fmt.Sprintf("/proc/%d/mem", pid), unix.O_RDONLY, 0o777)
	if err != nil {
		return nil, err
	}
	defer unix.Close(memfd)

	size, err := unix.Pread(memfd, buffer, int64(offset))
	if err != nil {
		return nil, err
	}

	return buffer[:size], nil
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

// getFdInProcess get the file descriptor in other process
func getFdInProcess(pid, targetFd int) (int, error) {
	targetPidfd, err := pidfd.Open(int(pid), 0)
	if err != nil {
		return 0, fmt.Errorf("pidfd Open failed: %s", err)
	}
	defer syscall.Close(int(targetPidfd))

	fd, err := targetPidfd.GetFd(targetFd, 0)
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

func readSockaddrFromProcess(pid uint32, offset uint64, addrlen uint64) (*sockaddr, error) {
	buf, err := readProcMem(pid, offset, addrlen)
	if err != nil {
		return nil, fmt.Errorf("failed readProcMem pid %v offset 0x%x: %s", pid, offset, err)
	}
	return newSockaddr(buf)
}

func (h *notifHandler) registerSocket(pid uint32, sockfd int) (*socketStatus, error) {
	logger := logrus.WithFields(logrus.Fields{"pid": pid, "sockfd": sockfd})
	proc, ok := h.processes[pid]
	if !ok {
		proc = newProcessStatus()
		h.processes[pid] = proc
		logger.Info("process is registered")
	}

	sock, ok := proc.sockets[sockfd]
	if ok {
		logger.Info("socket is already registered")
		return sock, nil
	}

	sockFdHost, err := getFdInProcess(int(pid), sockfd)
	if err != nil {
		return nil, err
	}
	defer syscall.Close(sockFdHost)

	sockDomain, sockType, sockProtocol, err := getSocketArgs(sockFdHost)
	sock = newSocketStatus(pid, sockfd, sockDomain, sockType, sockProtocol)
	if err != nil {
		// non-socket fd is not bypassable
		sock.state = NotBypassable
	} else {
		if sockDomain != syscall.AF_INET && sockDomain != syscall.AF_INET6 {
			// non IP sockets are not handled.
			sock.state = NotBypassable
		} else if sockType != syscall.SOCK_STREAM {
			// only accepting TCP socket
			sock.state = NotBypassable
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
	logger.Infof("socket is registered (state=%s)", sock.state)

	return sock, nil
}

func (h *notifHandler) getSocket(pid uint32, sockfd int) *socketStatus {
	proc, ok := h.processes[pid]
	if !ok {
		return nil
	}
	sock := proc.sockets[sockfd]
	return sock
}

func (h *notifHandler) removeSocket(pid uint32, sockfd int) {
	defer logrus.WithFields(logrus.Fields{"pid": pid, "sockfd": sockfd}).Infof("socket is removed")
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

	// cleanup sockets when the process exit.
	if syscallName == "_exit" || syscallName == "exit_group" {
		delete(h.processes, ctx.req.Pid)
		logrus.WithFields(logrus.Fields{"pid": ctx.req.Pid}).Infof("process is removed")
		return
	}

	// remove socket when closed
	if syscallName == "close" {
		h.removeSocket(ctx.req.Pid, int(ctx.req.Data.Args[0]))
		return
	}

	pid := ctx.req.Pid
	sockfd := int(ctx.req.Data.Args[0])
	sock := h.getSocket(pid, sockfd)
	if sock == nil {
		sock, err = h.registerSocket(pid, sockfd)
		if err != nil {
			logrus.Errorf("failed to register socket pid %d sockfd %d: %s", pid, sockfd, err)
			return
		}
	}

	switch sock.state {
	case NotBypassable, Bypassed:
		return
	default:
		// continue
	}

	switch syscallName {
	case "bind":
		sock.handleSysBind(h, ctx)
	case "connect":
		sock.handleSysConnect(h, ctx)
	case "setsockopt":
		sock.handleSysSetsockopt(ctx)
	case "fcntl":
		sock.handleSysFcntl(ctx)
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
	ignoredSubnets           []net.IPNet
	ignoredSubnetsAutoUpdate bool
	readyFd                  int

	// key is child port
	forwardingPorts map[int]ForwardPortMapping
}

// NewHandler creates new seccomp notif handler
func NewHandler(socketPath string) *Handler {
	handler := Handler{
		socketPath:      socketPath,
		ignoredSubnets:  []net.IPNet{},
		forwardingPorts: map[int]ForwardPortMapping{},
		readyFd:         -1,
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

type notifHandler struct {
	fd                      libseccomp.ScmpFd
	state                   *specs.ContainerProcessState
	nonBypassable           *nonbypassable.NonBypassable
	nonBypassableAutoUpdate bool
	forwardingPorts         map[int]ForwardPortMapping

	// key is pid
	processes map[uint32]*processStatus
}

func (h *Handler) newNotifHandler(fd uintptr, state *specs.ContainerProcessState) *notifHandler {
	notifHandler := notifHandler{
		fd:              libseccomp.ScmpFd(fd),
		state:           state,
		forwardingPorts: map[int]ForwardPortMapping{},
		processes:       map[uint32]*processStatus{},
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
func (h *Handler) StartHandle() {
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
		go notifHandler.handle()
	}
}
