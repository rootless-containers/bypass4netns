package bypass4netns

import (
	"fmt"
	"syscall"
	"unsafe"

	libseccomp "github.com/seccomp/libseccomp-golang"
	"github.com/vtolstov/go-ioctl"
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
