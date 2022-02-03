package main

// This code is copied from 'runc(https://github.com/opencontainers/runc/blob/v1.1.0/contrib/cmd/seccompagent/seccompagent.go)'
// The code is licensed under Apach-2.0 License

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
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

var (
	socketFile string
	pidFile    string
)

func closeStateFds(recvFds []int) {
	for i := range recvFds {
		unix.Close(i)
	}
}

func seccomp_iow(nr, typ uintptr) uintptr {
	return ioctl.IOW(uintptr(C.SECCOMP_IOC_MAGIC), nr, typ)
}

// C.SECCOMP_IOCTL_NOTIF_ADDFD become error
// Error Message: could not determine kind of name for C.SECCOMP_IOCTL_NOTIF_ADDFD
// TODO: use C.SECCOMP_IOCTL_NOTIF_ADDFD or add equivalent variable to libseccomp-go
func seccomp_ioctl_notif_addfd() uintptr {
	return seccomp_iow(3, uintptr(C.get_size_of_seccomp_notif_addfd()))
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

type Context struct {
	notifFd libseccomp.ScmpFd
	req     *libseccomp.ScmpNotifReq
	resp    *libseccomp.ScmpNotifResp
}

type SeccompNotifAddFd struct {
	id         uint64
	flags      uint32
	srcfd      uint32
	newfd      uint32
	newfdFlags uint32
}

func handleSysConnect(ctx *Context) {
	ctx.resp.Flags |= C.SECCOMP_USER_NOTIF_FLAG_CONTINUE

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
		//case 172:
		//	logrus.Infof("skipping (possibly) (`docker network create` network ip=%v", addrInet.Addr)
		//	return
	}

	targetPidfd, err := pidfd.Open(int(ctx.req.Pid), 0)
	if err != nil {
		logrus.Errorf("Pidfd Open failed: %s", err)
		return
	}
	defer syscall.Close(int(targetPidfd))

	sockfd, err := targetPidfd.GetFd(int(ctx.req.Data.Args[0]), 0)
	if err != nil {
		logrus.Errorf("Pidfd GetFd failed: %s", err)
		return
	}
	defer syscall.Close(sockfd)

	logrus.Debugf("got sockfd=%v", sockfd)
	sock_domain, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	if err != nil {
		logrus.Errorf("getsockopt(SO_DOMAIN) failed: %s", err)
		return
	}

	if sock_domain != syscall.AF_INET {
		logrus.Errorf("expected AF_INET, got %d", sock_domain)
		return
	}

	sock_type, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if err != nil {
		logrus.Errorf("getsockopt(SO_TYPE) failed: %s", err)
		return
	}

	sock_protocol, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
	if err != nil {
		logrus.Errorf("getsockopt(SO_PROTOCOL) failed: %s", err)
		return
	}

	sockfd2, err := syscall.Socket(sock_domain, sock_type, sock_protocol)
	if err != nil {
		logrus.Errorf("socket failed: %s", err)
	}
	defer syscall.Close(sockfd2)

	addfd := SeccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      C.SECCOMP_ADDFD_FLAG_SETFD,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	ioctl_op := seccomp_ioctl_notif_addfd()
	logrus.Debugf("ioctl_op:%v", ioctl_op)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(ctx.notifFd), ioctl_op, uintptr(unsafe.Pointer(&addfd)))
	if errno != 0 {
		logrus.Errorf("ioctl(SECCOMP_IOCTL_NOTIF_ADFD) failed: %s", err)
		return
	}
}

func handleSysBind(ctx *Context) {
	ctx.resp.Flags |= C.SECCOMP_USER_NOTIF_FLAG_CONTINUE

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
	}

	logrus.Debugf("%v", addrInet)
	port := ((addrInet.Port & 0xFF) << 8) | (addrInet.Port >> 8)
	logrus.Infof("bind(pid=%d): sockfd=%d, port=%d, ip=%v", ctx.req.Pid, ctx.req.Data.Args[0], port, addrInet.Addr)

	if port != 5201 {
		logrus.Infof("not mapped port=%d", port)
		return
	}

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

	targetPidfd, err := pidfd.Open(int(ctx.req.Pid), 0)
	if err != nil {
		logrus.Errorf("Pidfd Open failed: %s", err)
		return
	}
	defer syscall.Close(int(targetPidfd))

	sockfd, err := targetPidfd.GetFd(int(ctx.req.Data.Args[0]), 0)
	if err != nil {
		logrus.Errorf("Pidfd GetFd failed: %s", err)
		return
	}
	defer syscall.Close(sockfd)

	logrus.Debugf("got sockfd=%v", sockfd)
	sock_domain, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_DOMAIN)
	if err != nil {
		logrus.Errorf("getsockopt(SO_DOMAIN) failed: %s", err)
		return
	}

	if sock_domain != syscall.AF_INET {
		logrus.Errorf("expected AF_INET, got %d", sock_domain)
		return
	}

	sock_type, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_TYPE)
	if err != nil {
		logrus.Errorf("getsockopt(SO_TYPE) failed: %s", err)
		return
	}

	sock_protocol, err := syscall.GetsockoptInt(sockfd, syscall.SOL_SOCKET, syscall.SO_PROTOCOL)
	if err != nil {
		logrus.Errorf("getsockopt(SO_PROTOCOL) failed: %s", err)
		return
	}

	sockfd2, err := syscall.Socket(sock_domain, sock_type, sock_protocol)
	if err != nil {
		logrus.Errorf("socket failed: %s", err)
		return
	}
	defer syscall.Close(sockfd2)

	err = syscall.SetsockoptInt(sockfd2, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	if err != nil {
		logrus.Errorf("setsockopt(SO_REUSEADDR) failed: %s", err)
		return
	}

	bind_addr := syscall.SockaddrInet4{
		Port: int(8080),
		Addr: addrInet.Addr,
	}

	err = syscall.Bind(sockfd2, &bind_addr)
	if err != nil {
		logrus.Errorf("bind failed: %s", err)
		return
	}

	addfd := SeccompNotifAddFd{
		id:         ctx.req.ID,
		flags:      C.SECCOMP_ADDFD_FLAG_SETFD,
		srcfd:      uint32(sockfd2),
		newfd:      uint32(ctx.req.Data.Args[0]),
		newfdFlags: 0,
	}

	ioctl_op := seccomp_ioctl_notif_addfd()
	logrus.Debugf("ioctl_op:%v", ioctl_op)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(ctx.notifFd), ioctl_op, uintptr(unsafe.Pointer(&addfd)))
	if errno != 0 {
		logrus.Errorf("ioctl(SECCOMP_IOCTL_NOTIF_ADFD) failed: %s", err)
		return
	}
	ctx.resp.Flags &= (^uint32(C.SECCOMP_USER_NOTIF_FLAG_CONTINUE))
}

func handleReq(ctx *Context) {
	syscallName, err := ctx.req.Data.Syscall.GetName()
	if err != nil {
		logrus.Errorf("Error decoding syscall %v(): %s", ctx.req.Data.Syscall, err)
		// TODO: error handle
		return
	}
	logrus.Debugf("Received syscall %q, pid %v, arch %q, args %+v", syscallName, ctx.req.Pid, ctx.req.Data.Arch, ctx.req.Data.Args)

	switch syscallName {
	case "connect":
		handleSysConnect(ctx)
	case "bind":
		handleSysBind(ctx)
	default:
		logrus.Errorf("Unknown syscall %q", syscallName)
		// TODO: error handle
		return
	}

}

// notifHandler handles seccomp notifications and responses
func notifHandler(fd libseccomp.ScmpFd, metadata string) {
	defer unix.Close(int(fd))
	for {
		req, err := libseccomp.NotifReceive(fd)
		if err != nil {
			logrus.Errorf("Error in NotifReceive(): %s", err)
			continue
		}

		ctx := Context{
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

		handleReq(&ctx)

		if err = libseccomp.NotifRespond(fd, ctx.resp); err != nil {
			logrus.Errorf("Error in notification response: %s", err)
			continue
		}
	}
}

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

	logrus.Info("Waiting for seccomp file descriptors")
	l, err := net.Listen("unix", socketFile)
	if err != nil {
		logrus.Fatalf("Cannot listen: %s", err)
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
		newFd, metadata, err := handleNewMessage(int(socket.Fd()))
		socket.Close()
		if err != nil {
			logrus.Errorf("Error receiving seccomp file descriptor: %v", err)
			continue
		}

		// Make sure we don't allow strings like "/../p", as that means
		// a file in a different location than expected. We just want
		// safe things to use as a suffix for a file name.
		metadata = filepath.Base(metadata)
		if strings.Contains(metadata, "/") {
			// Fallback to a safe string.
			metadata = "agent-generated-suffix"
		}

		logrus.Infof("Received new seccomp fd: %v", newFd)
		go notifHandler(libseccomp.ScmpFd(newFd), metadata)
	}
}
