package bypass4netns

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/opencontainers/runtime-spec/specs-go"
	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/vtolstov/go-ioctl"
)

const (
	SeccompAddFdFlagSetFd        = 1
	SeccompUserNotifFlagContinue = 1
	SeccompIocMagic              = '!'
)

func seccompIOW(nr, typ uintptr) uintptr {
	return ioctl.IOW(uintptr(SeccompIocMagic), nr, typ)
}

// C.SECCOMP_IOCTL_NOTIF_ADDFD become error
// Error Message: could not determine kind of name for C.SECCOMP_IOCTL_NOTIF_ADDFD
// TODO: use C.SECCOMP_IOCTL_NOTIF_ADDFD or add equivalent variable to libseccomp-go
func seccompIoctlNotifAddfd() uintptr {
	return seccompIOW(3, uintptr(unsafe.Sizeof(seccompNotifAddFd{})))
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

func getDefaultSeccompProfile(listenerPath string) *specs.LinuxSeccomp {
	seccomp := &specs.LinuxSeccomp{
		DefaultAction: specs.ActAllow,
		Architectures: []specs.Arch{specs.ArchX86_64, specs.ArchX86, specs.ArchX32},
		ListenerPath:  listenerPath,
		Syscalls: []specs.LinuxSyscall{
			{
				Names:  []string{"bind", "close", "connect", "sendmsg", "sendto", "setsockopt"},
				Action: specs.ActNotify,
			},
		},
	}

	return seccomp
}
