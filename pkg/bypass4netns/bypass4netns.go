package bypass4netns

// This code is copied from 'runc(https://github.com/opencontainers/runc/blob/v1.1.0/contrib/cmd/seccompagent/seccompagent.go)'
// The code is licensed under Apache-2.0 License

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"syscall"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/oraoto/go-pidfd"
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

func handleNewMessage(sockfd int) (uintptr, string, error) {
	const maxNameLen = 4096
	stateBuf := make([]byte, maxNameLen)
	oobSpace := unix.CmsgSpace(4)
	oob := make([]byte, oobSpace)

	n, oobn, _, _, err := unix.Recvmsg(sockfd, stateBuf, oob, 0)
	if err != nil {
		return 0, "", err
	}
	if n >= maxNameLen || oobn != oobSpace {
		return 0, "", fmt.Errorf("recvfd: incorrect number of bytes read (n=%d oobn=%d)", n, oobn)
	}

	// Truncate.
	stateBuf = stateBuf[:n]
	oob = oob[:oobn]

	scms, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return 0, "", err
	}
	if len(scms) != 1 {
		return 0, "", fmt.Errorf("recvfd: number of SCMs is not 1: %d", len(scms))
	}
	scm := scms[0]

	fds, err := unix.ParseUnixRights(&scm)
	if err != nil {
		return 0, "", err
	}

	containerProcessState := &specs.ContainerProcessState{}
	err = json.Unmarshal(stateBuf, containerProcessState)
	if err != nil {
		closeStateFds(fds)
		return 0, "", fmt.Errorf("cannot parse OCI state: %w", err)
	}

	fd, err := parseStateFds(containerProcessState.Fds, fds)
	if err != nil {
		closeStateFds(fds)
		return 0, "", err
	}

	return fd, containerProcessState.Metadata, nil
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

// duplicateSocketOnHost duplicate socket in other process to socket on host.
// retun values are (duplicated socket fd, target socket fd in current process, error)
func duplicateSocketOnHost(pid int, _sockfd int) (int, int, error) {
	sockfd, err := getFdInProcess(pid, _sockfd)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get fd %s", err)
	}

	sock_domain, sock_type, sock_protocol, err := getSocketArgs(sockfd)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to get socket args %s", err)
	}

	if sock_domain != syscall.AF_INET {
		return 0, 0, fmt.Errorf("expected AF_INET, got %d", sock_domain)
	}

	// only SOCK_STREAM and SOCK_DGRAM are acceptable.
	if sock_type != syscall.SOCK_STREAM && sock_type != syscall.SOCK_DGRAM {
		return 0, 0, fmt.Errorf("SOCK_STREAM and SOCK_DGRAM are supported")
	}

	sockfd2, err := syscall.Socket(sock_domain, sock_type, sock_protocol)
	if err != nil {
		return 0, 0, fmt.Errorf("socket failed: %s", err)
	}

	return sockfd2, sockfd, nil
}

func readAddrInet4FromProcess(pid uint32, offset uint64, addrlen uint64) (*syscall.RawSockaddrInet4, error) {
	buf, err := readProcMem(pid, offset, addrlen)
	if err != nil {
		return nil, fmt.Errorf("failed readProcMem pid %v offset 0x%x: %s", pid, offset, err)
	}

	addr := syscall.RawSockaddr{}
	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &addr)
	if err != nil {
		return nil, fmt.Errorf("cannot cast byte array to RawSocksddr: %s", err)
	}

	if addr.Family != syscall.AF_INET {
		return nil, fmt.Errorf("not AF_INET addr: %d", addr.Family)
	}
	addrInet := syscall.RawSockaddrInet4{}
	reader.Seek(0, 0)
	err = binary.Read(reader, binary.LittleEndian, &addrInet)
	if err != nil {
		return nil, fmt.Errorf("cannot cast byte array to RawSockaddrInet4: %s", err)
	}

	return &addrInet, nil
}

// manageSocket manages socketStatus and return next injecting file descriptor
// return values are (continue?, injecting fd)
func (h *notifHandler) manageSocket(destAddr net.IP, pid int, sockfd int, logger *logrus.Entry) (bool, int) {
	destIsIgnored := h.isIgnored(destAddr)
	key := fmt.Sprintf("%d:%d", pid, sockfd)
	sockStatus, ok := h.socketInfo.status[key]
	if !ok {
		if destIsIgnored {
			// the socket has never been bypassed and no need to bypass
			logger.Debugf("%s is ignored, skipping.", destAddr.String())
			return false, 0
		} else {
			// the socket has never been bypassed and need to bypass
			sockfd2, sockfd, err := duplicateSocketOnHost(pid, sockfd)
			if err != nil {
				logger.Errorf("duplicating socket failed: %s", err)
				return false, 0
			}

			sockStatus := socketStatus{
				state:     Bypassed,
				fdInNetns: sockfd,
				fdInHost:  sockfd2,
			}
			h.socketInfo.status[key] = sockStatus
			logger.Debugf("start to bypass fdInHost=%d fdInNetns=%d", sockStatus.fdInHost, sockStatus.fdInNetns)
			return true, sockfd2
		}
	} else {
		if sockStatus.state == Bypassed {
			if !destIsIgnored {
				// the socket has been bypassed and continue to be bypassed
				logger.Debugf("continue to bypass")
				return false, 0
			} else {
				// the socket has been bypassed and need to switch back to socket in netns
				logger.Debugf("switchback fdInHost(%d) -> fdInNetns(%d)", sockStatus.fdInHost, sockStatus.fdInNetns)
				sockStatus.state = SwitchBacked

				h.socketInfo.status[key] = sockStatus
				return true, sockStatus.fdInNetns
			}
		} else if sockStatus.state == SwitchBacked {
			if destIsIgnored {
				// the socket has been switchbacked(not bypassed) and no need to be bypassed
				logger.Debugf("continue not bypassing")
				return false, 0
			} else {
				// the socket has been switchbacked(not bypassed) and need to bypass again
				logger.Debugf("bypass again fdInNetns(%d) -> fdInHost(%d)", sockStatus.fdInNetns, sockStatus.fdInHost)
				sockStatus.state = Bypassed

				h.socketInfo.status[key] = sockStatus
				return true, sockStatus.fdInHost
			}
		} else {
			panic(fmt.Errorf("unexpected state :%d", sockStatus.state))
		}
	}
}

// handleSysBind handles syscall bind(2).
// If binding port is the target of port-forwarding,
// it creates and configures including bind(2) a socket on host.
// Then, handler replaces container's socket to created one.
func (h *notifHandler) handleSysBind(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "bind", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	addrInet, err := readAddrInet4FromProcess(ctx.req.Pid, ctx.req.Data.Args[1], ctx.req.Data.Args[2])
	if err != nil {
		logger.Errorf("failed to read addrInet4 from process: %s", err)
		return
	}

	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logger.Infof("handle port=%d, ip=%v", port, addrInet.Addr)

	// TODO: get port-fowrad mapping from nerdctl
	fwdPort, ok := h.forwardingPorts[int(port)]
	if !ok {
		logger.Infof("port=%d is not target of port forwarding.", port)
		return
	}

	sockfd2, sockfd, err := duplicateSocketOnHost(int(ctx.req.Pid), int(ctx.req.Data.Args[0]))
	if err != nil {
		logger.Errorf("duplicating socket failed: %s", err)
		return
	}
	defer syscall.Close(sockfd)
	defer syscall.Close(sockfd2)

	err = h.socketInfo.configureSocket(ctx, sockfd2)
	if err != nil {
		syscall.Close(sockfd2)
		logger.Errorf("configure socketoptions failed: %s", err)
		return
	}

	bind_addr := syscall.SockaddrInet4{
		Port: fwdPort.HostPort,
		Addr: addrInet.Addr,
	}

	err = syscall.Bind(sockfd2, &bind_addr)
	if err != nil {
		logger.Errorf("bind failed: %s", err)
		return
	}

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      SeccompAddFdFlagSetFd,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logger.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}

	logger.Infof("binding for %d:%d is done", fwdPort.HostPort, fwdPort.ChildPort)

	ctx.resp.Flags &= (^uint32(SeccompUserNotifFlagContinue))
}

// handleSysClose handles `close(2)` and delete recorded socket options.
func (h *notifHandler) handleSysClose(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "close", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	logger.Trace("handle")
	h.socketInfo.deleteSocket(ctx, logger)
}

// handleSysConnect handles syscall connect(2).
// If destination is outside of container network,
// it creates and configures a socket on host.
// Then, handler replaces container's socket to created one.
func (h *notifHandler) handleSysConnect(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "connect", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	addrInet, err := readAddrInet4FromProcess(ctx.req.Pid, ctx.req.Data.Args[1], ctx.req.Data.Args[2])
	if err != nil {
		logger.Errorf("failed to read addrInet4 from process: %s", err)
		return
	}

	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logger.Infof("handle port=%d, ip=%v", port, addrInet.Addr)

	// TODO: more sophisticated way to convert.
	ipAddr := net.IPv4(addrInet.Addr[0], addrInet.Addr[1], addrInet.Addr[2], addrInet.Addr[3])

	// Retrieve next injecting file descriptor
	cont, sockfd2 := h.manageSocket(ipAddr, int(ctx.req.Pid), int(ctx.req.Data.Args[0]), logger)

	if !cont {
		return
	}

	// configure socket if switched
	err = h.socketInfo.configureSocket(ctx, sockfd2)
	if err != nil {
		syscall.Close(sockfd2)
		logger.Errorf("configure socketoptions failed: %s", err)
		return
	}

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      SeccompAddFdFlagSetFd,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logger.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}
}

type msgHdrName struct {
	Name    uint64
	Namelen uint32
}

// handleSysSendto handles syscall sendmsg(2).
// If destination is outside of container network,
// it creates and configures a socket on host.
// Then, handler replaces container's socket to created one.
// This handles only SOCK_DGRAM sockets.
func (h *notifHandler) handleSysSendmsg(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "sendmsg", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	msghdr := msgHdrName{}
	buf, err := readProcMem(ctx.req.Pid, ctx.req.Data.Args[1], 12)
	if err != nil {
		logger.Errorf("failed readProcMem pid %v offset 0x%x: %s", ctx.req.Pid, ctx.req.Data.Args[1], err)
	}

	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &msghdr)
	if err != nil {
		logger.Errorf("cannnot cast byte array to Msghdr: %s", err)
	}

	// addrlen == 0 means the socket is already connected
	if msghdr.Namelen == 0 {
		return
	}

	sockfd, err := getFdInProcess(int(ctx.req.Pid), int(ctx.req.Data.Args[0]))
	if err != nil {
		logger.Errorf("failed to get fd: %s", err)
	}
	sock_domain, sock_type, _, err := getSocketArgs(sockfd)

	if err != nil {
		logger.Error("failed to get socket args: %s", err)
		return
	}

	if sock_domain != syscall.AF_INET {
		logger.Debug("only supported AF_INET: %d")
		return
	}

	if sock_type != syscall.SOCK_DGRAM {
		logger.Debug("only SOCK_DGRAM sockets are handled")
		return
	}

	addrOffset := uint64(msghdr.Name)
	addrInet, err := readAddrInet4FromProcess(ctx.req.Pid, addrOffset, uint64(msghdr.Namelen))
	if err != nil {
		logger.Errorf("failed to read addrInet4 from process: %s", err)
		return
	}

	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logger.Infof("handle port=%d, ip=%v", port, addrInet.Addr)

	// TODO: more sophisticated way to convert.
	ipAddr := net.IPv4(addrInet.Addr[0], addrInet.Addr[1], addrInet.Addr[2], addrInet.Addr[3])

	// Retrieve next injecting file descriptor
	cont, sockfd2 := h.manageSocket(ipAddr, int(ctx.req.Pid), int(ctx.req.Data.Args[0]), logger)

	if !cont {
		return
	}

	// configure socket if switched
	err = h.socketInfo.configureSocket(ctx, sockfd2)
	if err != nil {
		syscall.Close(sockfd2)
		logger.Errorf("setsocketoptions failed: %s", err)
		return
	}

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      SeccompAddFdFlagSetFd,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logger.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}
}

// handleSysSendto handles syscall sendto(2).
// If destination is outside of container network,
// it creates and configures a socket on host.
// Then, handler replaces container's socket to created one.
// This handles only SOCK_DGRAM sockets.
func (h *notifHandler) handleSysSendto(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "sendto", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	// addrlen == 0 is send(2)
	if ctx.req.Data.Args[5] == 0 {
		return
	}

	sockfd, err := getFdInProcess(int(ctx.req.Pid), int(ctx.req.Data.Args[0]))
	if err != nil {
		logger.Errorf("failed to get fd: %s", err)
	}
	sock_domain, sock_type, _, err := getSocketArgs(sockfd)

	if err != nil {
		logger.Error("failed to get socket args: %s", err)
		return
	}

	if sock_domain != syscall.AF_INET {
		logger.Debug("only supported AF_INET: %d")
		return
	}

	if sock_type != syscall.SOCK_DGRAM {
		logger.Debug("only SOCK_DGRAM sockets are handled")
		return
	}

	addrInet, err := readAddrInet4FromProcess(ctx.req.Pid, ctx.req.Data.Args[4], ctx.req.Data.Args[5])
	if err != nil {
		logger.Errorf("failed to read addrInet4 from process: %s", err)
		return
	}

	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logger.Infof("handle port=%d, ip=%v", port, addrInet.Addr)

	// TODO: more sophisticated way to convert.
	ipAddr := net.IPv4(addrInet.Addr[0], addrInet.Addr[1], addrInet.Addr[2], addrInet.Addr[3])

	// Retrieve next injecting file descriptor
	cont, sockfd2 := h.manageSocket(ipAddr, int(ctx.req.Pid), int(ctx.req.Data.Args[0]), logger)

	if !cont {
		return
	}

	// configure socket if switched
	err = h.socketInfo.configureSocket(ctx, sockfd2)
	if err != nil {
		syscall.Close(sockfd2)
		logger.Errorf("configure socketoptions failed: %s", err)
		return
	}

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      SeccompAddFdFlagSetFd,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logger.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}
}

// handleSyssetsockopt handles `setsockopt(2)` and records options.
// Recorded options are used in `handleSysConnect` or `handleSysBind` via `setSocketoptions` to configure created sockets.
func (h *notifHandler) handleSysSetsockopt(ctx *context) {
	logger := logrus.WithFields(logrus.Fields{"syscall": "setsockopt", "pid": ctx.req.Pid, "sockfd": ctx.req.Data.Args[0]})
	logger.Debugf("handle")
	err := h.socketInfo.recordSocketOption(ctx, logger)
	if err != nil {
		logger.Errorf("recordSocketOption failed: %s", err)
	}
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

	switch syscallName {
	case "bind":
		h.handleSysBind(ctx)
	case "close":
		// handling close(2) may cause performance degradation
		h.handleSysClose(ctx)
	case "connect":
		h.handleSysConnect(ctx)
	case "sendmsg":
		h.handleSysSendmsg(ctx)
	case "sendto":
		h.handleSysSendto(ctx)
	case "setsockopt":
		h.handleSysSetsockopt(ctx)
	default:
		logrus.Errorf("Unknown syscall %q", syscallName)
		// TODO: error handle
		return
	}

}

// notifHandler handles seccomp notifications and response to them.
func (h *notifHandler) handle() {
	defer unix.Close(int(h.fd))

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
	socketPath     string
	ignoredSubnets []net.IPNet
	readyFd        int

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
func (h *Handler) SetIgnoredSubnets(subnets []net.IPNet) {
	h.ignoredSubnets = subnets
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
	fd              libseccomp.ScmpFd
	ignoredSubnets  []net.IPNet
	forwardingPorts map[int]ForwardPortMapping
	socketInfo      socketInfo
}

func (h *Handler) newNotifHandler(fd uintptr) *notifHandler {
	notifHandler := notifHandler{
		fd:              libseccomp.ScmpFd(fd),
		forwardingPorts: map[int]ForwardPortMapping{},
		socketInfo: socketInfo{
			options: map[string][]socketOption{},
			status:  map[string]socketStatus{},
		},
	}
	notifHandler.ignoredSubnets = make([]net.IPNet, len(h.ignoredSubnets))
	// Deep copy []net.IPNet because each thread accesses it.
	copy(notifHandler.ignoredSubnets, h.ignoredSubnets)

	// Deep copy of map
	for key, value := range h.forwardingPorts {
		notifHandler.forwardingPorts[key] = value
	}

	return &notifHandler
}

// isIgnored checks the IP address is ignored.
func (h *notifHandler) isIgnored(ip net.IP) bool {
	for _, subnet := range h.ignoredSubnets {
		if subnet.Contains(ip) {
			return true
		}
	}

	return false
}

// StartHandle starts seccomp notif handler
func (h *Handler) StartHandle() {
	logrus.Info("Waiting for seccomp file descriptors")
	l, err := net.Listen("unix", h.socketPath)
	if err != nil {
		logrus.Fatalf("Cannot listen: %w", err)
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
		newFd, _, err := handleNewMessage(int(socket.Fd()))
		socket.Close()
		if err != nil {
			logrus.Errorf("Error receiving seccomp file descriptor: %v", err)
			continue
		}

		logrus.Infof("Received new seccomp fd: %v", newFd)
		notifHandler := h.newNotifHandler(newFd)
		go notifHandler.handle()
	}
}
