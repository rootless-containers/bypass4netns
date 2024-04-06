package util

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

// shrinkID shrinks id to short(12 chars) id
// 6d9bcda7cebd551ddc9e3173d2139386e21b56b241f8459c950ef58e036f6bd8
// to
// 6d9bcda7cebd
func ShrinkID(id string) string {
	if len(id) < 12 {
		return id
	}

	return id[0:12]
}

func SameUserNS(pidX, pidY int) (bool, error) {
	return sameNS(pidX, pidY, "user")
}

func SameNetNS(pidX, pidY int) (bool, error) {
	return sameNS(pidX, pidY, "net")
}

func sameNS(pidX, pidY int, nsName string) (bool, error) {
	nsX := fmt.Sprintf("/proc/%d/ns/%s", pidX, nsName)
	nsY := fmt.Sprintf("/proc/%d/ns/%s", pidY, nsName)
	nsXResolved, err := os.Readlink(nsX)
	if err != nil {
		return false, err
	}
	nsYResolved, err := os.Readlink(nsY)
	if err != nil {
		return false, err
	}
	return nsXResolved == nsYResolved, nil
}

// copied from https://github.com/pfnet-research/meta-fuse-csi-plugin/blob/437dbbbbf16e5b02f9a508e3403d044b0a9dff89/pkg/util/fdchannel.go#L29
// which is licensed under apache 2.0
func SendMsg(via net.Conn, fd int, msg []byte) error {
	conn, ok := via.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("failed to cast via to *net.UnixConn")
	}
	connf, err := conn.File()
	if err != nil {
		return err
	}
	socket := int(connf.Fd())
	defer connf.Close()

	rights := syscall.UnixRights(fd)

	return syscall.Sendmsg(socket, msg, rights, nil, 0)
}

func RecvMsg(via net.Conn) (int, []byte, error) {
	conn, ok := via.(*net.UnixConn)
	if !ok {
		return 0, nil, fmt.Errorf("failed to cast via to *net.UnixConn")
	}
	connf, err := conn.File()
	if err != nil {
		return 0, nil, err
	}
	socket := int(connf.Fd())
	defer connf.Close()

	buf := make([]byte, syscall.CmsgSpace(4))
	b := make([]byte, 500)
	//nolint:dogsled
	n, _, _, _, err := syscall.Recvmsg(socket, b, buf, 0)
	if err != nil {
		return 0, nil, err
	}

	var msgs []syscall.SocketControlMessage
	msgs, err = syscall.ParseSocketControlMessage(buf)
	if err != nil {
		return 0, nil, err
	}

	fds, err := syscall.ParseUnixRights(&msgs[0])
	if err != nil {
		return 0, nil, err
	}

	return fds[0], b[:n], err
}
