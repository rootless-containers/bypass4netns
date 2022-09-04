package oci

import (
	"fmt"
	"reflect"

	"github.com/opencontainers/runtime-spec/specs-go"
)

const (
	SocketName = "bypass4netns.sock"
)

var SyscallsToBeNotified = []string{"bind", "close", "connect", "sendmsg", "sendto", "setsockopt"}

func GetDefaultSeccompProfile(listenerPath string) *specs.LinuxSeccomp {
	tmpl := specs.LinuxSeccomp{
		DefaultAction: specs.ActAllow,
	}
	seccomp, err := TranslateSeccompProfile(tmpl, listenerPath)
	if err != nil {
		panic(err)
	}
	return seccomp
}

func TranslateSeccompProfile(old specs.LinuxSeccomp, listenerPath string) (*specs.LinuxSeccomp, error) {
	sc := old
	if sc.ListenerPath != "" && sc.ListenerPath != listenerPath {
		return nil, fmt.Errorf("bypass4netns's seccomp listener path %q conflicts with the existing seccomp listener path %q", listenerPath, sc.ListenerPath)
	}
	sc.ListenerPath = listenerPath
	prepend := specs.LinuxSyscall{
		Names:  SyscallsToBeNotified,
		Action: specs.ActNotify,
	}
	if alreadyPrepended := len(sc.Syscalls) > 0 && reflect.DeepEqual(sc.Syscalls[0], prepend); !alreadyPrepended {
		for i := range sc.Syscalls {
			i := i
			sc.Syscalls[i].Names = filterStringSlice(sc.Syscalls[i].Names, SyscallsToBeNotified)
		}
		sc.Syscalls = append([]specs.LinuxSyscall{prepend}, sc.Syscalls...)
	}
	return &sc, nil
}

func filterStringSlice(ss, banned []string) []string {
	bannedM := make(map[string]struct{}, len(banned))
	for _, f := range banned {
		bannedM[f] = struct{}{}
	}
	var res []string
	for _, s := range ss {
		if _, ok := bannedM[s]; !ok {
			res = append(res, s)
		}
	}
	return res
}
