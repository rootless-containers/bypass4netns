package nsagent

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"sort"

	"github.com/rootless-containers/bypass4netns/pkg/bypass4netns/nsagent/types"
	"github.com/sirupsen/logrus"
	"golang.org/x/sys/unix"
)

func Main() error {
	w := os.Stdout
	if err := inspect(w); err != nil {
		return err
	}
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, unix.SIGHUP, unix.SIGTERM, unix.SIGINT)
	for sig := range sigCh {
		switch sig {
		case unix.SIGHUP:
			if err := inspect(w); err != nil {
				return err
			}
		case unix.SIGTERM, unix.SIGINT:
			return nil
		}
	}
	return nil
}

func inspect(w io.Writer) error {
	var msg types.Message
	interfaces, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("failed to enumerate the network interfaces: %w", err)
	}
	for _, intf := range interfaces {
		addrs, err := intf.Addrs()
		if err != nil {
			logrus.Warnf("Failed to get the addresses of the network interface %q: %v", intf.Name, err)
			continue
		}
		entry := types.Interface{
			Name: intf.Name,
		}
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok {
				entry.CIDRs = append(entry.CIDRs, ipNet.String())
			}
		}
		sort.Strings(entry.CIDRs)
		msg.Interfaces = append(msg.Interfaces, entry)
	}
	sort.Slice(msg.Interfaces, func(i, j int) bool {
		return msg.Interfaces[i].Name < msg.Interfaces[j].Name
	})
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	b = append(b, []byte("\n")...)
	_, err = w.Write(b)
	return err
}
