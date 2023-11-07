package types

type Message struct {
	Interfaces []Interface `json:"interfaces"` // sorted by Name
}

type Interface struct {
	Name   string   `json:"name"` // "lo", "eth0", etc.
	HWAddr string   `json:"hwAddr"`
	CIDRs  []string `json:"cidrs"` // sorted as strings
}
