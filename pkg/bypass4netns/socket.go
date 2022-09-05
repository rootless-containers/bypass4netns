package bypass4netns

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/sirupsen/logrus"
)

type socketOption struct {
	level   uint64
	optname uint64
	optval  []byte
	optlen  uint64
}

type socketState int

const (
	// Bypassed means that the socket is replaced by one created on the host
	Bypassed socketState = iota

	// SwitchBacked means that the socket was bypassed but now rereplaced to the socket in netns.
	// This state can be hannpend in connect(2), sendto(2) and sendmsg(2)
	// when connecting to a host outside of netns and then connecting to a host inside of netns with same fd.
	SwitchBacked
)

type socketStatus struct {
	state     socketState
	fdInNetns int
	fdInHost  int
}

type socketInfo struct {
	options map[string][]socketOption
	status  map[string]socketStatus
}

// configureSocket set recorded socket options.
func (info *socketInfo) configureSocket(ctx *context, sockfd int) error {
	key := fmt.Sprintf("%d:%d", ctx.req.Pid, ctx.req.Data.Args[0])
	optValues, ok := info.options[key]
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
func (info *socketInfo) recordSocketOption(ctx *context, logger *logrus.Entry) error {
	sockfd := ctx.req.Data.Args[0]
	level := ctx.req.Data.Args[1]
	optname := ctx.req.Data.Args[2]
	optlen := ctx.req.Data.Args[4]
	optval, err := readProcMem(ctx.req.Pid, ctx.req.Data.Args[3], optlen)
	if err != nil {
		return fmt.Errorf("readProcMem failed pid %v offset 0x%x: %s", ctx.req.Pid, ctx.req.Data.Args[1], err)
	}

	key := fmt.Sprintf("%d:%d", ctx.req.Pid, sockfd)
	_, ok := info.options[key]
	if !ok {
		info.options[key] = make([]socketOption, 0)
	}

	value := socketOption{
		level:   level,
		optname: optname,
		optval:  optval,
		optlen:  optlen,
	}
	info.options[key] = append(info.options[key], value)

	logger.Debugf("recorded socket option sockfd=%d level=%d optname=%d optval=%v optlen=%d", sockfd, level, optname, optval, optlen)
	return nil
}

// deleteSocketOptions delete recorded socket options and status
func (info *socketInfo) deleteSocket(ctx *context, logger *logrus.Entry) {
	sockfd := ctx.req.Data.Args[0]
	key := fmt.Sprintf("%d:%d", ctx.req.Pid, sockfd)
	_, ok := info.options[key]
	if ok {
		delete(info.options, key)
		logger.Debugf("removed socket options")
	}

	status, ok := info.status[key]
	if ok {
		delete(info.status, key)
		syscall.Close(status.fdInNetns)
		syscall.Close(status.fdInHost)
		logger.Debugf("removed socket status(fdInNetns=%d fdInHost=%d)", status.fdInNetns, status.fdInHost)
	}
}
