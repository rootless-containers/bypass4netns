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
