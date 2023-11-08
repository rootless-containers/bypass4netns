package tracer

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/sirupsen/logrus"
)

type TracerCommand struct {
	Cmd                TracerCmd `json:"tracerCmd"`
	ForwardingPorts    []int     `json:"forwardingPorts,omitempty"`
	DestinationAddress []string  `json:"destinationAddress,omitempty"`
}

type TracerCmd int

const (
	Ok TracerCmd = iota
	RegisterForwardPorts
	ConnectToAddress
)

func Main() error {
	r := os.Stdin
	w := os.Stdout
	dec := json.NewDecoder(r)
	for {
		var cmd TracerCommand
		err := dec.Decode(&cmd)
		if err != nil {
			logrus.WithError(err).Errorf("failed to decode")
			break
		}
		logrus.Infof("decoded = %v", cmd)
		switch cmd.Cmd {
		case RegisterForwardPorts:
			for _, p := range cmd.ForwardingPorts {
				readyChan := make(chan bool)
				go func(port int, c chan bool) {
					err := listenLoop(port, c)
					if err != nil {
						logrus.WithError(err).Errorf("failed to listen on port %d", port)
					}
				}(p, readyChan)
				<-readyChan
			}
			cmd = TracerCommand{
				Cmd: Ok,
			}
		case ConnectToAddress:
			addrs := []string{}
			for i := range cmd.DestinationAddress {
				addr := cmd.DestinationAddress[i]
				err = tryToConnect(addr)
				if err != nil {
					logrus.WithError(err).Warnf("failed to connect to %s", addr)
					continue
				}
				addrs = append(addrs, addr)
			}
			cmd = TracerCommand{
				Cmd:                Ok,
				DestinationAddress: addrs,
			}
		}

		m, err := json.Marshal(cmd)
		if err != nil {
			logrus.WithError(err).Errorf("failed to encode")
		}
		_, err = w.Write(m)
		if err != nil {
			logrus.WithError(err).Errorf("failed to write")
		}
	}

	logrus.Infof("Exit.")
	return nil
}

func listenLoop(port int, readyChan chan bool) error {
	l, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}
	defer l.Close()

	readyChan <- true
	logrus.Infof("started to listen on port %d", port)
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		conn.Close()
	}
}

func tryToConnect(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 10*time.Millisecond)
	if err != nil {
		return err
	}
	defer conn.Close()
	logrus.Infof("successfully connected to %s", addr)

	return nil
}
