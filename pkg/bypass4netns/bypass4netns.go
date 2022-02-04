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
	"unsafe"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/oraoto/go-pidfd"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/sirupsen/logrus"
	"github.com/vtolstov/go-ioctl"
	"golang.org/x/sys/unix"
)

/*
#include <linux/types.h>
#include <seccomp.h>

int get_size_of_seccomp_notif_addfd() {
	return sizeof(struct seccomp_notif_addfd);
}
*/
import "C"

func seccompIOW(nr, typ uintptr) uintptr {
	return ioctl.IOW(uintptr(C.SECCOMP_IOC_MAGIC), nr, typ)
}

// C.SECCOMP_IOCTL_NOTIF_ADDFD become error
// Error Message: could not determine kind of name for C.SECCOMP_IOCTL_NOTIF_ADDFD
// TODO: use C.SECCOMP_IOCTL_NOTIF_ADDFD or add equivalent variable to libseccomp-go
func seccompIoctlNotifAddfd() uintptr {
	return seccompIOW(3, uintptr(C.get_size_of_seccomp_notif_addfd()))
}

type seccompNotifAddFd struct {
	id         uint64
	flags      uint32
	srcfd      uint32
	newfd      uint32
	newfdFlags uint32
}

func (addfd *seccompNotifAddFd) ioctlNotifAddFd(notifFd libseccomp.ScmpFd) error {
	ioctl_op := seccompIoctlNotifAddfd()
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(notifFd), ioctl_op, uintptr(unsafe.Pointer(addfd)))
	if errno != 0 {
		return fmt.Errorf("ioctl(SECCOMP_IOCTL_NOTIF_ADFD) failed: %s", errno)
	}
	return nil
}

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

type socketOption struct {
	level   uint64
	optname uint64
	optval  []byte
	optlen  uint64
}

type socketOptions struct {
	options map[string][]socketOption
}

// configureSocket set recorded socket options.
func (opts *socketOptions) configureSocket(ctx *context, sockfd int) error {
	key := fmt.Sprintf("%d:%d", ctx.req.Pid, ctx.req.Data.Args[0])
	optValues, ok := opts.options[key]
	if !ok {
		return nil
	}
	for _, optVal := range optValues {
		_, _, errno := syscall.Syscall6(syscall.SYS_SETSOCKOPT, uintptr(sockfd), uintptr(optVal.level), uintptr(optVal.optname), uintptr(unsafe.Pointer(&optVal.optval[0])), uintptr(optVal.optlen), 0)
		if errno != 0 {
			return fmt.Errorf("setsockopt failed(%v): %s", optVal, errno)
		}
		logrus.Debugf("configured socket option pid=%d sockfd=%d (%v)", ctx.req.Pid, sockfd, optVal)
	}

	return nil
}

// recordSocketOption records socket option.
func (opts *socketOptions) recordSocketOption(ctx *context) error {
	sockfd := ctx.req.Data.Args[0]
	level := ctx.req.Data.Args[1]
	optname := ctx.req.Data.Args[2]
	optlen := ctx.req.Data.Args[4]
	optval, err := readProcMem(ctx.req.Pid, ctx.req.Data.Args[3], optlen)
	if err != nil {
		return fmt.Errorf("readProcMem failed pid %v offset 0x%x: %s", ctx.req.Pid, ctx.req.Data.Args[1], err)
	}

	key := fmt.Sprintf("%d:%d", ctx.req.Pid, sockfd)
	_, ok := opts.options[key]
	if !ok {
		opts.options[key] = make([]socketOption, 0)
	}

	value := socketOption{
		level:   level,
		optname: optname,
		optval:  optval,
		optlen:  optlen,
	}
	opts.options[key] = append(opts.options[key], value)

	logrus.Debugf("recorded socket option sockfd=%d level=%d optname=%d optval=%v optlen=%d", sockfd, level, optname, optval, optlen)
	return nil
}

// deleteSocketOptions delete recorded socket options
func (opts *socketOptions) deleteSocketOptions(ctx *context) {
	sockfd := ctx.req.Data.Args[0]
	key := fmt.Sprintf("%d:%d", ctx.req.Pid, sockfd)
	_, ok := opts.options[key]
	if ok {
		delete(opts.options, key)
		logrus.Debugf("removed socket options(pid=%d sockfd=%d key=%s)", ctx.req.Pid, sockfd, key)
	}
}

type context struct {
	notifFd libseccomp.ScmpFd
	req     *libseccomp.ScmpNotifReq
	resp    *libseccomp.ScmpNotifResp
}

// duplicateSocketOnHost duplicate socket in other process to socket on host.
func duplicateSocketOnHost(ctx *context, opts *socketOptions) (int, error) {
	targetPidfd, err := pidfd.Open(int(ctx.req.Pid), 0)
	if err != nil {
		return 0, fmt.Errorf("pidfd Open failed: %s", err)
	}
	defer syscall.Close(int(targetPidfd))

	sockfd, err := targetPidfd.GetFd(int(ctx.req.Data.Args[0]), 0)
	if err != nil {
		return 0, fmt.Errorf("pidfd GetFd failed: %s", err)
	}
	defer syscall.Close(sockfd)

	logrus.Debugf("got sockfd=%v", sockfd)
	sock_domain, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	if err != nil {
		return 0, fmt.Errorf("getsockopt(SO_DOMAIN) failed: %s", err)
	}

	if sock_domain != syscall.AF_INET {
		return 0, fmt.Errorf("expected AF_INET, got %d", sock_domain)
	}

	sock_type, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if err != nil {
		return 0, fmt.Errorf("getsockopt(SO_TYPE) failed: %s", err)
	}

	sock_protocol, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
	if err != nil {
		return 0, fmt.Errorf("getsockopt(SO_PROTOCOL) failed: %s", err)
	}

	sockfd2, err := syscall.Socket(sock_domain, sock_type, sock_protocol)
	if err != nil {
		return 0, fmt.Errorf("socket failed: %s", err)
	}

	err = opts.configureSocket(ctx, sockfd2)
	if err != nil {
		syscall.Close(sockfd2)
		return 0, fmt.Errorf("setsocketoptions failed: %s", err)
	}

	return sockfd2, nil
}

// handleSysConnect handles syscall connect(2).
// If destination is outside of container network,
// it creates and configures a socket on host.
// Then, handler replaces container's socket to created one.
func handleSysConnect(ctx *context, opts *socketOptions) {
	addrlen := ctx.req.Data.Args[2]
	buf, err := readProcMem(ctx.req.Pid, ctx.req.Data.Args[1], addrlen)
	if err != nil {
		logrus.Errorf("Error readProcMem pid %v offset 0x%x: %s", ctx.req.Pid, ctx.req.Data.Args[1], err)
		return
	}

	addr := syscall.RawSockaddr{}
	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &addr)
	if err != nil {
		logrus.Errorf("Error casting byte array to RawSocksddr: %s", err)
		return
	}

	if addr.Family != syscall.AF_INET {
		logrus.Debugf("Not AF_INET addr: %d", addr.Family)
		return
	}
	addrInet := syscall.RawSockaddrInet4{}
	reader.Seek(0, 0)
	err = binary.Read(reader, binary.LittleEndian, &addrInet)
	if err != nil {
		logrus.Errorf("Error casting byte array to RawSockaddrInet4: %s", err)
		return
	}

	logrus.Debugf("%v", addrInet)
	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logrus.Infof("connect(pid=%d): sockfd=%d, port=%d, ip=%v", ctx.req.Pid, ctx.req.Data.Args[0], port, addrInet.Addr)

	switch addrInet.Addr[0] {
	case 127:
		logrus.Infof("skipping local ip=%v", addrInet.Addr)
		return
	case 10:
		logrus.Infof("skipping (possibly) (`podmain|nerdctl) network create` network ip=%v", addrInet.Addr)
		return
	case 172:
		logrus.Infof("skipping (possibly) (`docker network create` network ip=%v", addrInet.Addr)
		return
	}

	sockfd2, err := duplicateSocketOnHost(ctx, opts)
	if err != nil {
		logrus.Errorf("duplicating socket failed: %s", err)
		return
	}
	defer syscall.Close(sockfd2)

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      C.SECCOMP_ADDFD_FLAG_SETFD,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logrus.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}
}

// handleSysBind handles syscall bind(2).
// If binding port is the target of port-forwarding,
// it creates and configures including bind(2) a socket on host.
// Then, handler replaces container's socket to created one.
func handleSysBind(ctx *context, opts *socketOptions) {
	addrlen := ctx.req.Data.Args[2]
	buf, err := readProcMem(ctx.req.Pid, ctx.req.Data.Args[1], addrlen)
	if err != nil {
		logrus.Errorf("Error readProcMem pid %v offset 0x%x: %s", ctx.req.Pid, ctx.req.Data.Args[1], err)
		return
	}

	addr := syscall.RawSockaddr{}
	reader := bytes.NewReader(buf)
	err = binary.Read(reader, binary.LittleEndian, &addr)
	if err != nil {
		logrus.Errorf("Error casting byte array to RawSocksddr: %s", err)
		return
	}

	if addr.Family != syscall.AF_INET {
		logrus.Debugf("Not AF_INET addr: %d", addr.Family)
		return
	}
	addrInet := syscall.RawSockaddrInet4{}
	reader.Seek(0, 0)
	err = binary.Read(reader, binary.LittleEndian, &addrInet)
	if err != nil {
		logrus.Errorf("Error casting byte array to RawSockaddrInet4: %s", err)
		return
	}

	logrus.Debugf("%v", addrInet)
	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logrus.Infof("bind(pid=%d): sockfd=%d, port=%d, ip=%v", ctx.req.Pid, ctx.req.Data.Args[0], port, addrInet.Addr)

	// TODO: get port-fowrad mapping from nerdctl
	if port != 5201 {
		logrus.Infof("not mapped port=%d", port)
		return
	}

	sockfd2, err := duplicateSocketOnHost(ctx, opts)
	if err != nil {
		logrus.Errorf("duplicating socket failed: %s", err)
		return
	}
	defer syscall.Close(sockfd2)

	bind_addr := syscall.SockaddrInet4{
		Port: int(8080),
		Addr: addrInet.Addr,
	}

	err = syscall.Bind(sockfd2, &bind_addr)
	if err != nil {
		logrus.Errorf("bind failed: %s", err)
		return
	}

	addfd := seccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      C.SECCOMP_ADDFD_FLAG_SETFD,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	err = addfd.ioctlNotifAddFd(ctx.notifFd)
	if err != nil {
		logrus.Errorf("ioctl NotifAddFd failed: %s", err)
		return
	}

	ctx.resp.Flags &= (^uint32(C.SECCOMP_USER_NOTIF_FLAG_CONTINUE))
}

// handleSyssetsockopt handles `setsockopt(2)` and records options.
// Recorded options are used in `handleSysConnect` or `handleSysBind` via `setSocketoptions` to configure created sockets.
func handleSysSetsockopt(ctx *context, opts *socketOptions) {
	logrus.Debugf("setsockopt(pid=%d): sockfd=%d", ctx.req.Pid, ctx.req.Data.Args[0])
	err := opts.recordSocketOption(ctx)
	if err != nil {
		logrus.Errorf("recordSocketOption failed: %s", err)
	}
}

// handleSysClose handles `close(2)` and delete recorded socket options.
func handleSysClose(ctx *context, opts *socketOptions) {
	sockfd := ctx.req.Data.Args[0]
	logrus.Debugf("close(pid=%d): sockfd=%d", ctx.req.Pid, sockfd)
	opts.deleteSocketOptions(ctx)
}

// handleReq handles seccomp notif requests and configures responses.
func handleReq(ctx *context, opts *socketOptions) {
	syscallName, err := ctx.req.Data.Syscall.GetName()
	if err != nil {
		logrus.Errorf("Error decoding syscall %v(): %s", ctx.req.Data.Syscall, err)
		// TODO: error handle
		return
	}
	logrus.Debugf("Received syscall %q, pid %v, arch %q, args %+v", syscallName, ctx.req.Pid, ctx.req.Data.Arch, ctx.req.Data.Args)

	ctx.resp.Flags |= C.SECCOMP_USER_NOTIF_FLAG_CONTINUE

	switch syscallName {
	case "connect":
		handleSysConnect(ctx, opts)
	case "bind":
		handleSysBind(ctx, opts)
	case "setsockopt":
		handleSysSetsockopt(ctx, opts)
	case "close":
		// handling close(2) may cause performance degradation
		handleSysClose(ctx, opts)
	default:
		logrus.Errorf("Unknown syscall %q", syscallName)
		// TODO: error handle
		return
	}

}

// notifHandler handles seccomp notifications and response to them.
func notifHandler(fd libseccomp.ScmpFd) {
	defer unix.Close(int(fd))
	opts := socketOptions{
		options: map[string][]socketOption{},
	}

	for {
		req, err := libseccomp.NotifReceive(fd)
		if err != nil {
			logrus.Errorf("Error in NotifReceive(): %s", err)
			continue
		}

		ctx := context{
			notifFd: fd,
			req:     req,
			resp: &libseccomp.ScmpNotifResp{
				ID:    req.ID,
				Error: 0,
				Val:   0,
				Flags: libseccomp.NotifRespFlagContinue,
			},
		}

		// TOCTOU check
		if err := libseccomp.NotifIDValid(fd, req.ID); err != nil {
			logrus.Errorf("TOCTOU check failed: req.ID is no longer valid: %s", err)
			continue
		}

		handleReq(&ctx, &opts)

		if err = libseccomp.NotifRespond(fd, ctx.resp); err != nil {
			logrus.Errorf("Error in notification response: %s", err)
			continue
		}
	}
}

type Handler struct {
	socketPath string
}

// NewHandler creates new seccomp notif handler
func NewHandler(socketPath string) *Handler {
	handler := Handler{
		socketPath: socketPath,
	}

	return &handler
}

// StartHandle starts seccomp notif handler
func (h *Handler) StartHandle() {
	logrus.Info("Waiting for seccomp file descriptors")
	l, err := net.Listen("unix", h.socketPath)
	if err != nil {
		logrus.Fatalf("Cannot listen: %w", err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
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
		go notifHandler(libseccomp.ScmpFd(newFd))
	}
}
