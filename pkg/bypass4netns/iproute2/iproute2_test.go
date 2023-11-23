package iproute2

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUnmarshalAddress(t *testing.T) {
	testJson := `
[
   {
      "ifindex":1,
      "ifname":"lo",
      "flags":[
         "LOOPBACK",
         "UP",
         "LOWER_UP"
      ],
      "mtu":65536,
      "qdisc":"noqueue",
      "operstate":"UNKNOWN",
      "group":"default",
      "txqlen":1000,
      "link_type":"loopback",
      "address":"00:00:00:00:00:00",
      "broadcast":"00:00:00:00:00:00",
      "addr_info":[
         {
            "family":"inet",
            "local":"127.0.0.1",
            "prefixlen":8,
            "scope":"host",
            "label":"lo",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         },
         {
            "family":"inet6",
            "local":"::1",
            "prefixlen":128,
            "scope":"host",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         }
      ]
   },
   {
      "ifindex":2,
      "ifname":"enp1s0",
      "flags":[
         "BROADCAST",
         "MULTICAST",
         "UP",
         "LOWER_UP"
      ],
      "mtu":1500,
      "qdisc":"fq_codel",
      "operstate":"UP",
      "group":"default",
      "txqlen":1000,
      "link_type":"ether",
      "address":"52:54:00:c3:92:b6",
      "broadcast":"ff:ff:ff:ff:ff:ff",
      "addr_info":[
         {
            "family":"inet",
            "local":"192.168.1.155",
            "prefixlen":24,
            "broadcast":"192.168.1.255",
            "scope":"global",
            "label":"enp1s0",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         },
         {
            "family":"inet6",
            "local":"fe80::5054:ff:fec3:92b6",
            "prefixlen":64,
            "scope":"link",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         }
      ]
   },
   {
      "ifindex":3,
      "ifname":"docker0",
      "flags":[
         "NO-CARRIER",
         "BROADCAST",
         "MULTICAST",
         "UP"
      ],
      "mtu":1500,
      "qdisc":"noqueue",
      "operstate":"DOWN",
      "group":"default",
      "link_type":"ether",
      "address":"02:42:ab:c8:78:84",
      "broadcast":"ff:ff:ff:ff:ff:ff",
      "addr_info":[
         {
            "family":"inet",
            "local":"172.17.0.1",
            "prefixlen":16,
            "broadcast":"172.17.255.255",
            "scope":"global",
            "label":"docker0",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         }
      ]
   },
   {
      "ifindex":61,
      "ifname":"lxdbr0",
      "flags":[
         "BROADCAST",
         "MULTICAST",
         "UP",
         "LOWER_UP"
      ],
      "mtu":1500,
      "qdisc":"noqueue",
      "operstate":"UP",
      "group":"default",
      "txqlen":1000,
      "link_type":"ether",
      "address":"00:16:3e:4d:92:98",
      "broadcast":"ff:ff:ff:ff:ff:ff",
      "addr_info":[
         {
            "family":"inet",
            "local":"192.168.6.1",
            "prefixlen":24,
            "scope":"global",
            "label":"lxdbr0",
            "valid_life_time":4294967295,
            "preferred_life_time":4294967295
         }
      ]
   },
   {
      "ifindex":71,
      "link_index":70,
      "ifname":"veth71db11e7",
      "flags":[
         "BROADCAST",
         "MULTICAST",
         "UP",
         "LOWER_UP"
      ],
      "mtu":1500,
      "qdisc":"noqueue",
      "master":"lxdbr0",
      "operstate":"UP",
      "group":"default",
      "txqlen":1000,
      "link_type":"ether",
      "address":"da:83:f0:97:c7:14",
      "broadcast":"ff:ff:ff:ff:ff:ff",
      "link_netnsid":0,
      "addr_info":[
         
      ]
   }
]	
	`

	addrs, err := UnmarshalAddress([]byte(testJson))
	assert.Equal(t, nil, err)
	assert.Equal(t, 5, len(addrs))
	intf := addrs[1]
	assert.Equal(t, "UP", intf.Operstate)
	assert.Equal(t, "ether", intf.LinkType)
	assert.Equal(t, 2, len(intf.AddrInfos))
	addr := intf.AddrInfos[0]
	assert.Equal(t, "inet", addr.Family)
	assert.Equal(t, "192.168.1.155", addr.Local)
	addrIp, addrCidr, err := net.ParseCIDR(fmt.Sprintf("%s/%d", addr.Local, addr.PrefixLen))
	assert.Equal(t, nil, err)
	addrCidr.IP = addrIp
	assert.Equal(t, "192.168.1.155/24", addrCidr.String())
	addr2 := intf.AddrInfos[1]
	assert.Equal(t, "inet6", addr2.Family)
	assert.Equal(t, "fe80::5054:ff:fec3:92b6", addr2.Local)
}
