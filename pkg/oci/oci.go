package oci

import (
	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	SocketName = "bypass4netns.sock"
)

func GetDefaultSeccompProfile(listenerPath string) *specs.LinuxSeccomp {
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
