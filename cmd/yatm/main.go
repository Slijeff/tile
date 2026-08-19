package main

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"yatm/internal/client"
	"yatm/internal/proto"
	"yatm/internal/server"
)

func main() {
	cmd := ""
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	var err error
	switch cmd {
	case "", "attach":
		err = attach()
	case "__server": // internal: the daemon itself
		err = server.RunServer()
	case "kill-server":
		err = killServer()
	default:
		fmt.Fprintf(os.Stderr, "usage: yatm [attach|kill-server]\n")
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "yatm:", err)
		os.Exit(1)
	}
}

// attach connects to the server, starting one if nothing answers.
func attach() error {
	sock, err := proto.SocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		os.Remove(sock) // a leftover file from a server that died
		if err := spawnServer(); err != nil {
			return err
		}
		for range 100 {
			time.Sleep(20 * time.Millisecond)
			if conn, err = net.Dial("unix", sock); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("server did not come up: %w", err)
		}
	}
	defer conn.Close()

	_, err = tea.NewProgram(client.New(conn)).Run()
	return err
}

// spawnServer re-execs this binary as a detached daemon.
func spawnServer() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	sock, err := proto.SocketPath()
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(sock), "server.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()

	c := exec.Command(exe, "__server")
	c.Stdin = nil
	c.Stdout, c.Stderr = log, log
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

func killServer() error {
	sock, err := proto.SocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return fmt.Errorf("no server running")
	}
	defer conn.Close()
	return client.New(conn).Send(proto.ClientMsg{Type: proto.MsgKill})
}
