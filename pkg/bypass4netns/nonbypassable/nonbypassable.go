package nonbypassable

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/rootless-containers/bypass4netns/pkg/api/com"
	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/nsagent/types"
	"github.com/rootless-containers/bypass4netns/pkg/util"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func New(staticList []net.IPNet) *NonBypassable {
	x := &NonBypassable{
		staticList: staticList,
	}
	return x
}

// NonBypassable maintains the list of the non-bypassable CIDRs,
// such as 127.0.0.0/8 and CNI bridge CIDRs in the slirp's network namespace.
type NonBypassable struct {
	staticList     []net.IPNet
	dynamicList    []net.IPNet
	interfaces     []com.Interface
	lastUpdateUnix int64
	mu             sync.RWMutex
}

func (x *NonBypassable) Contains(ip net.IP) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, subnet := range append(x.staticList, x.dynamicList...) {
		if subnet.Contains(ip) {
			return true
		}
	}
	return false
}

func (x *NonBypassable) IsInterfaceIPAddress(ip net.IP) bool {
	x.mu.RLock()
	defer x.mu.RUnlock()
	for _, intf := range x.interfaces {
		for _, intfIP := range intf.Addresses {
			if intfIP.IP.Equal(ip) {
				return true
			}
		}
	}

	return false
}

func (x *NonBypassable) GetInterfaces() []com.Interface {
	x.mu.RLock()
	defer x.mu.RUnlock()
	ips := append([]com.Interface{}, x.interfaces...)
	return ips
}

func (x *NonBypassable) GetLastUpdateUnix() int64 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.lastUpdateUnix
}

// WatchNS watches the NS associated with the PID and updates the internal dynamic list on receiving SIGHUP.
func (x *NonBypassable) WatchNS(ctx context.Context, pid int) error {
	selfExe, err := os.Executable()
	if err != nil {
		return err
	}
	nsenter, err := exec.LookPath("nsenter")
	if err != nil {
		return err
	}
	nsenterFlags := []string{
		"-t", strconv.Itoa(pid),
		"-F",
		"-n",
	}
	selfPid := os.Getpid()
	ok, err := util.SameUserNS(pid, selfPid)
	if err != nil {
		return fmt.Errorf("failed to check sameUserNS(%d, %d)", pid, selfPid)
	}
	if !ok {
		nsenterFlags = append(nsenterFlags, "-U", "--preserve-credentials")
	}
	nsenterFlags = append(nsenterFlags, "--", selfExe, "--nsagent")
	cmd := exec.CommandContext(ctx, nsenter, nsenterFlags...)
	cmd.SysProcAttr = &unix.SysProcAttr{
		Pdeathsig: unix.SIGTERM,
	}
	cmd.Stderr = os.Stderr
	r, w := io.Pipe()
	cmd.Stdout = w
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start %v: %w", cmd.Args, err)
	}
	cmdPid := cmd.Process.Pid
	logrus.Infof("Dynamic non-bypassable list: started NSAgent (PID=%d, target PID=%d)", cmdPid, pid)
	go x.watchNS(r)

	// > It is allowed to call Notify multiple times with different channels and the same signals:
	// > each channel receives copies of incoming signals independently.
	// https://pkg.go.dev/os/signal#Notify
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, unix.SIGHUP)
	for sig := range sigCh {
		if uSig, ok := sig.(unix.Signal); ok {
			_ = unix.Kill(cmdPid, uSig)
		}
	}
	return nil
}

func (x *NonBypassable) watchNS(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		var msg types.Message
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			logrus.WithError(err).Warnf("Dynamic non-bypassable list: Failed to parse nsagent message %q", line)
			continue
		}
		var newList []net.IPNet
		var newInterfaces []com.Interface
		for _, intf := range msg.Interfaces {
			i := com.Interface{
				Name:       intf.Name,
				Addresses:  make([]net.IPNet, 0),
				IsLoopback: false,
			}
			for _, cidr := range intf.CIDRs {
				ip, ipNet, err := net.ParseCIDR(cidr)
				if err != nil {
					logrus.WithError(err).Warnf("Dynamic non-bypassable list: Failed to parse nsagent message %q: %q: bad CIDR %q", line, intf.Name, cidr)
					continue
				}
				if ipNet != nil {
					newList = append(newList, *ipNet)
				}
				if ip.IsLoopback() {
					i.IsLoopback = true
				}
				ifIPNet := net.IPNet{
					IP:   ip,
					Mask: ipNet.Mask,
				}
				i.Addresses = append(i.Addresses, ifIPNet)
			}
			if !i.IsLoopback {
				var err error
				i.HWAddr, err = net.ParseMAC(intf.HWAddr)
				if err != nil {
					logrus.WithError(err).Errorf("invalid hardware address %q ifName=%s is ignored", intf.HWAddr, intf.Name)
				}
			}
			newInterfaces = append(newInterfaces, i)
		}
		x.mu.Lock()
		logrus.Infof("Dynamic non-bypassable list: old dynamic=%v, new dynamic=%v, static=%v", x.dynamicList, newList, x.staticList)
		x.dynamicList = newList
		x.interfaces = newInterfaces
		x.lastUpdateUnix = time.Now().Unix()
		x.mu.Unlock()
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, io.EOF) {
			logrus.WithError(err).Warn("Dynamic non-bypassable list: Error while parsing nsagent messages")
		}
	}
}
