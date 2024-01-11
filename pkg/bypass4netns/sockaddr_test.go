package bypass4netns

import (
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSerializeDeserializeSockaddr4(t *testing.T) {
	ip := net.ParseIP("192.168.1.100")
	port := 12345

	sa := sockaddr{
		IP:   ip,
		Port: port,
	}
	sa.Family = syscall.AF_INET

	saBytes, err := sa.toBytes()
	assert.Equal(t, nil, err)
	assert.Equal(t, 16, len(saBytes))

	sa2, err := newSockaddr(saBytes)
	assert.Equal(t, nil, err)
	assert.Equal(t, ip.String(), sa2.IP.String())
	assert.Equal(t, port, sa2.Port)
}

func TestSerializeDeserializeSockaddr6(t *testing.T) {
	ip := net.ParseIP("2001:0db8::1:0:0:1")
	port := 12345

	sa := sockaddr{
		IP:       ip,
		Port:     port,
		Flowinfo: 0x12345678,
		ScopeID:  0x9abcdef0,
	}
	sa.Family = syscall.AF_INET6

	saBytes, err := sa.toBytes()
	assert.Equal(t, nil, err)
	assert.Equal(t, 28, len(saBytes))

	sa2, err := newSockaddr(saBytes)
	assert.Equal(t, nil, err)
	assert.Equal(t, ip.String(), sa2.IP.String())
	assert.Equal(t, port, sa2.Port)
	assert.Equal(t, sa.Flowinfo, uint32(0x12345678))
	assert.Equal(t, sa.ScopeID, uint32(0x9abcdef0))
}
