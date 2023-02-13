package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	trackerAddr = &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 7701}
)

var cmdRoot = &cobra.Command{
	Use: "tt send file",
}

func init() {
	cmdRoot.AddCommand(cmdTracker, cmdSend, cmdReceive)
}

var cmdTracker = &cobra.Command{
	Use:  "tracker",
	RunE: cmdRunTracker,
}

func cmdRunTracker(cmd *cobra.Command, args []string) error {
	peer := make(map[uuid.UUID]net.Addr)

	l, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.IPv4(0, 0, 0, 0),
		Port: 7701,
	})
	if err != nil {
		panic(err)
	}
	buf := make([]byte, 1024)
	for {
		n, addr, err := l.ReadFromUDP(buf)
		if err != nil {
			panic(err)
		}
		pkt := buf[:n]
		cmdIndex := bytes.IndexByte(pkt, ' ')

		fmt.Printf("recv %s\n", pkt)

		switch string(pkt[:cmdIndex]) {
		case "#HAVE":
			id, err := uuid.Parse(string(pkt[cmdIndex+1:]))
			if err != nil {
				return err
			}
			peer[id] = addr
			fmt.Printf("%s has %s\n", addr, id)
		case "#WANT":
			id, err := uuid.Parse(string(pkt[cmdIndex+1:]))
			if err != nil {
				return err
			}
			if _, err := l.WriteTo([]byte(peer[id].String()), addr); err != nil {
				return err
			}
		}
	}
}

var cmdSend = &cobra.Command{
	Use:  "send file",
	RunE: cmdRunSend,
	Args: cobra.ExactArgs(1),
}

func cmdRunSend(cmd *cobra.Command, args []string) error {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}

	id := uuid.New()

	done := make(chan struct{})

	go func() {
		for {
			buf := make([]byte, 1024)
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				panic(err)
			}
			pkt := buf[:n]
			fmt.Printf("extra msg from %s: %s\n", addr, pkt)

			fd, err := os.Open(args[0])
			if err != nil {
				panic(err)
			}

			for {
				n, err := fd.Read(buf)
				if err != nil {
					if errors.Is(err, io.EOF) {
						close(done)
						return
					}
					panic(err)
				}
				if _, err := conn.WriteTo(buf[:n], addr); err != nil {
					panic(err)
				}
			}
		}
	}()

	notifyHave := func() {
		_, err := conn.WriteTo([]byte(fmt.Sprintf("#HAVE %s", id)), trackerAddr)
		if err != nil {
			panic(err)
		}
	}

	notifyHave()
	fmt.Printf("use \n\ttt receive %s\nto receive file\n", id)

	pingTicker := time.NewTicker(time.Second * 15)
	defer pingTicker.Stop()
	for {
		select {
		case <-pingTicker.C:
			notifyHave()
		case <-done:
			return nil
		}
	}
}

var cmdReceive = &cobra.Command{
	Use:  "receive file",
	RunE: cmdRunReceive,
	Args: cobra.ExactArgs(1),
}

func cmdRunReceive(cmd *cobra.Command, args []string) error {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		return err
	}

	id := args[0]

	if _, err := conn.WriteTo([]byte(fmt.Sprintf("#WANT %s", args[0])), trackerAddr); err != nil {
		panic(err)
	}

	buf := make([]byte, 1024)
	n, addr, err := conn.ReadFromUDP(buf)
	if err != nil {
		panic(err)
	}
	pkt := buf[:n]
	fmt.Printf("recv from tracker %s has %s\n", addr, pkt)

	peerAddr, err := net.ResolveUDPAddr("udp", string(pkt))
	if err != nil {
		return err
	}

	if _, err := conn.WriteTo([]byte(fmt.Sprintf("#GET %s 0 -1", id)), peerAddr); err != nil {
		return err
	}

	fd, err := os.Create(id)
	if err != nil {
		return err
	}
	defer fd.Close()

	for {
		fmt.Println("wait for file piece...")
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}

		if _, err := fd.Write(buf[:n]); err != nil {
			return err
		}

		if n < 1024 {
			break
		}
	}

	return nil
}

func main() {
	if err := cmdRoot.Execute(); err != nil {
		return
	}
}
