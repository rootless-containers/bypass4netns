#!/bin/bash
# FIXME: use autoconf/automake

# requires libseccomp >= v2.5.0
: ${LIBSECCOMP_PREFIX:=/opt/libseccomp}

set -eux -o pipefail
mkdir -p ./bin
gcc -o ./bin/bypass4netns -I${LIBSECCOMP_PREFIX}/include $(pkg-config --cflags glib-2.0) *.c ${LIBSECCOMP_PREFIX}/lib/libseccomp.a $(pkg-config --libs glib-2.0)
