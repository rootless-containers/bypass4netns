package bypass4netns

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
)

type sockaddr struct {
	syscall.RawSockaddr
	IP       net.IP
	Port     int
	Flowinfo uint32 // sin6_flowinfo
	ScopeID  uint32 // sin6_scope_id
}

func newSockaddr(buf []byte) (*sockaddr, error) {
	sa := &sockaddr{}
	reader := bytes.NewReader(buf)
	// TODO: support big endian hosts
	endian := binary.LittleEndian
	if err := binary.Read(reader, endian, &sa.RawSockaddr); err != nil {
		return nil, fmt.Errorf("cannot cast byte array to RawSocksddr: %w", err)
	}
	switch sa.Family {
	case syscall.AF_INET:
		addr4 := syscall.RawSockaddrInet4{}
		if _, err := reader.Seek(0, 0); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, endian, &addr4); err != nil {
			return nil, fmt.Errorf("cannot cast byte array to RawSockaddrInet4: %w", err)
		}
		sa.IP = make(net.IP, len(addr4.Addr))
		for i, x := range addr4.Addr { // nolint: gosimple
			sa.IP[i] = x
		}
		p := make([]byte, 2)
		binary.BigEndian.PutUint16(p, addr4.Port)
		sa.Port = int(endian.Uint16(p))
	case syscall.AF_INET6:
		addr6 := syscall.RawSockaddrInet6{}
		if _, err := reader.Seek(0, 0); err != nil {
			return nil, err
		}
		if err := binary.Read(reader, endian, &addr6); err != nil {
			return nil, fmt.Errorf("cannot cast byte array to RawSockaddrInet6: %w", err)
		}
		sa.IP = make(net.IP, len(addr6.Addr))
		for i, x := range addr6.Addr { // nolint: gosimple
			sa.IP[i] = x
		}
		p := make([]byte, 2)
		binary.BigEndian.PutUint16(p, addr6.Port)
		sa.Port = int(endian.Uint16(p))
		sa.Flowinfo = addr6.Flowinfo
		sa.ScopeID = addr6.Scope_id
	default:
		return nil, fmt.Errorf("expected AF_INET or AF_INET6, got %d", sa.Family)
	}
	return sa, nil
}
