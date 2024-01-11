package com

import (
	"net"
)

type ContainerInterfaces struct {
	ContainerID string      `json:"containerID"`
	Interfaces  []Interface `json:"interfaces"`
	// key is "container-side" port, value is host-side port
	ForwardingPorts map[int]int `json:"forwardingPorts"`
}
type Interface struct {
	Name       string           `json:"name"`
	HWAddr     net.HardwareAddr `json:"hwAddr"`
	Addresses  []net.IPNet      `json:"addresses"`
	IsLoopback bool             `json:"isLoopback"`
}
