#!/bin/sh
# Usage:
# $ ./seccomp.json.sh >$HOME/seccomp.json
# $ nerdctl run -it --rm --security-opt seccomp=$HOME/seccomp.json alpine

# TODO: support non-x86
# TODO: inherit the default seccomp profile (https://github.com/containerd/containerd/blob/v1.6.0-rc.1/contrib/seccomp/seccomp_default.go#L52)

set -eu
cat <<EOF
{
  "defaultAction": "SCMP_ACT_ALLOW",
  "architectures": [
    "SCMP_ARCH_X86_64",
    "SCMP_ARCH_X86",
    "SCMP_ARCH_X32"
  ],
  "listenerPath": "${XDG_RUNTIME_DIR}/bypass4netns.sock",
  "syscalls": [
    {
      "names": [
        "bind",
        "close",
        "connect",
        "sendto",
        "setsockopt"
      ],
      "action": "SCMP_ACT_NOTIFY"
    }
  ]
}
EOF
