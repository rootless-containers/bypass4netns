package iproute2

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strconv"

	"github.com/rootless-containers/bypass4netns/pkg/util"
	"golang.org/x/sys/unix"
)

type AddrInfo struct {
	Family            string `json:"family"`
	Local             string `json:"local"`
	PrefixLen         int    `json:"prefixlen"`
	Broadcast         string `json:"broadcast"`
	Scope             string `json:"scope"`
	Label             string `json:"label"`
	ValidLifeTime     int    `json:"valid_life_time"`
	PreferredLifeTime int    `json:"preferred_life_time"`
}

type Interface struct {
	IfIndex   int        `json:"ifindex"`
	IfName    string     `json:"ifname"`
	Flags     []string   `json:"flags"`
	Mtu       int        `json:"mtu"`
	Qdisc     string     `json:"noqueue"`
	Operstate string     `json:"operstate"`
	Group     string     `json:"group"`
	TxQLen    int        `json:"txqlen"`
	LinkType  string     `json:"link_type"`
	Address   string     `json:"address"`
	Broadcast string     `json:"broadcast"`
	AddrInfos []AddrInfo `json:"addr_info"`
}

type Addresses = []Interface

func UnmarshalAddress(jsonAddrs []byte) (Addresses, error) {
	var addrs = Addresses{}

	err := json.Unmarshal(jsonAddrs, &addrs)
	if err != nil {
		return nil, err
	}

	return addrs, nil
}

func GetAddressesInNetNS(ctx context.Context, pid int) (Addresses, error) {
	nsenter, err := exec.LookPath("nsenter")
	if err != nil {
		return nil, err
	}
	nsenterFlags := []string{
		"-t", strconv.Itoa(pid),
		"-F",
		"-n",
	}
	selfPid := os.Getpid()
	ok, err := util.SameUserNS(pid, selfPid)
	if err != nil {
		return nil, fmt.Errorf("failed to check sameUserNS(%d, %d)", pid, selfPid)
	}
	if !ok {
		nsenterFlags = append(nsenterFlags, "-U", "--preserve-credentials")
	}
	nsenterFlags = append(nsenterFlags, "--", "ip", "-j", "addr", "show")
	cmd := exec.CommandContext(ctx, nsenter, nsenterFlags...)
	cmd.SysProcAttr = &unix.SysProcAttr{
		Pdeathsig: unix.SIGTERM,
	}
	stdout, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to start %v: %w", cmd.Args, err)
	}

	addrs, err := UnmarshalAddress(stdout)
	if err != nil {
		return nil, fmt.Errorf("failed to parse json: %w", err)
	}

	return addrs, nil
}
