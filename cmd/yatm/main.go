package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

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
	case "-h", "--help", "help":
		_, err = os.Stdout.WriteString(usage) // not fmt.Print: %<id> reads as a verb
	default:
		// Everything else is a one-shot command for a running server. The
		// arguments go over untouched: the server owns the whole command
		// surface, so there is one place to add a command rather than two.
		err = runCommand(cmd, os.Args[2:])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "yatm:", err)
		os.Exit(1)
	}
}

// usage lives here rather than behind a "help" command because it has to work
// with no server running, which is exactly when someone needs it.
const usage = `yatm — terminal multiplexer

  yatm [attach]                    attach to the session, starting it if needed
  yatm kill-server                 stop the session and every shell in it

Commands for a running session. Panes are %<id>, windows @<id>, as printed
by "yatm list":

  yatm list [--json]               every window and pane
  yatm capture   %p [--lines N]    a pane's text, no escape codes
  yatm send-keys %p [--key SPEC] [--enter] [text...]
  yatm split     %p [-h|-v]        split a pane, prints the new pane's id
  yatm stack     %p                layer a pane behind it, prints its id
  yatm new-window                  prints the new window's id
  yatm kill-pane   %p
  yatm kill-window @w
  yatm focus       %p
  yatm resize      %p <left|right|up|down> <cells>
  yatm rename  %p|@w [name]        blank name reverts to the shell's title
`

// runCommand sends one command to the running server and prints its answer.
func runCommand(cmd string, cmdArgs []string) error {
	sock, err := proto.SocketPath()
	if err != nil {
		return err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		return fmt.Errorf("no server running")
	}
	defer conn.Close()

	// This connection never sends MsgAttach, so it does not displace whoever
	// is attached — the session carries on untouched while we ask.
	c := newClient(conn)
	if err := c.send(proto.ClientMsg{Type: proto.MsgCmd, Cmd: cmd, Args: cmdArgs}); err != nil {
		return err
	}
	var reply proto.ServerMsg
	if err := json.NewDecoder(conn).Decode(&reply); err != nil {
		return fmt.Errorf("no reply from server: %w", err)
	}
	if reply.Err != "" {
		return errors.New(reply.Err)
	}
	if reply.Content != "" {
		fmt.Println(reply.Content)
	}
	return nil
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

	// Ask to attach explicitly. Connecting alone doesn't: one-shot commands
	// share this socket and must leave the attached session where it is.
	m := newClient(conn)
	if err := m.send(proto.ClientMsg{Type: proto.MsgAttach}); err != nil {
		return err
	}
	_, err = tea.NewProgram(m).Run()
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

// killServer stops the daemon and every shell in it. No prompt: the in-app
// quit binding is the one that asks, and typing "kill-server" is already
// saying it.
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
	return newClient(conn).send(proto.ClientMsg{Type: proto.MsgKill})
}
