package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "charm.land/bubbletea/v2"

	"tile/internal/proto"
	"tile/internal/server"
)

func main() {
	name, explicit, rest := splitSession(os.Args[1:])
	cmd := ""
	if len(rest) > 0 {
		cmd = rest[0]
	}
	var err error
	switch cmd {
	case "", "attach":
		err = attach(name)
	case "__server": // internal: the daemon itself, given its session name as argv[1]
		err = server.RunServer(rest[1])
	case "kill-server":
		err = killServer(name, explicit)
	case "ls", "list-sessions":
		err = listSessions()
	case "-h", "--help", "help":
		_, err = os.Stdout.WriteString(usage) // not fmt.Print: %<id> reads as a verb
	default:
		// Everything else is a one-shot command for a running server. The
		// arguments go over untouched: the server owns the whole command
		// surface, so there is one place to add a command rather than two.
		err = runCommand(cmd, rest[1:], name)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "tile:", err)
		os.Exit(1)
	}
}

// splitSession pulls "-t name" / "--session name" / "--session=name" out of
// the argument list, wherever it appears, so every command shares one way to
// pick a session rather than each needing its own flag parsing. explicit
// reports whether the flag was present at all, which kill-server needs to
// tell "-t default" from no flag.
func splitSession(args []string) (name string, explicit bool, rest []string) {
	for i, a := range args {
		if v, ok := strings.CutPrefix(a, "--session="); ok {
			return v, true, append(append([]string{}, args[:i]...), args[i+1:]...)
		}
		if (a == "-t" || a == "--session") && i+1 < len(args) {
			return args[i+1], true, append(append([]string{}, args[:i]...), args[i+2:]...)
		}
	}
	return "", false, args
}

// usage lives here rather than behind a "help" command because it has to work
// with no server running, which is exactly when someone needs it.
const usage = `tile — terminal multiplexer

  tile [attach] [-t name]          attach to a session, starting it if needed
  tile kill-server [-t name]       stop a session, or every session (asks first)
  tile ls                          list running sessions

"-t name" (any command) targets a session other than "default".

Commands for a running session. Panes are %<id>, windows @<id>, as printed
by "tile list":

  tile list [--json]               every window and pane
  tile capture   %p [--lines N]    a pane's text, no escape codes
  tile send-keys %p [--key SPEC] [--enter] [text...]
  tile split     %p [-h|-v]        split a pane, prints the new pane's id
  tile stack     %p                layer a pane behind it, prints its id
  tile new-window                  prints the new window's id
  tile kill-pane   %p
  tile kill-window @w
  tile focus       %p
  tile resize      %p <left|right|up|down> <cells>
  tile even    %p|@w               equal shares: the pane's branch, or a whole window
  tile rename  %p|@w [name]        blank name reverts to the shell's title
`

// runCommand sends one command to the named session's server and prints its
// answer.
func runCommand(cmd string, cmdArgs []string, session string) error {
	sock, err := proto.SocketPath(session)
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

// attach connects to the named session's server, starting one if nothing
// answers, and keeps going for as long as the sessions picker (s p) asks
// to hop to another running session: a switch is a plain detach that
// names where to reconnect next, so each pass here is its own attachOnce
// against a fresh connection.
func attach(session string) error {
	for {
		next, err := attachOnce(session)
		if err != nil || next == "" {
			return err
		}
		session = next
	}
}

// attachOnce dials one session's server, starting it if nothing answers,
// and runs the client program against it until it detaches. The returned
// name is blank for a plain detach or quit, or the session the sessions
// picker asked to switch to.
func attachOnce(session string) (string, error) {
	sock, err := proto.SocketPath(session)
	if err != nil {
		return "", err
	}
	conn, err := net.Dial("unix", sock)
	if err != nil {
		os.Remove(sock) // a leftover file from a server that died
		if err := spawnServer(session); err != nil {
			return "", err
		}
		for range 100 {
			time.Sleep(20 * time.Millisecond)
			if conn, err = net.Dial("unix", sock); err == nil {
				break
			}
		}
		if err != nil {
			return "", fmt.Errorf("server did not come up: %w", err)
		}
	}
	defer conn.Close()

	// Ask to attach explicitly. Connecting alone doesn't: one-shot commands
	// share this socket and must leave the attached session where it is.
	m := newClient(conn)
	if err := m.send(proto.ClientMsg{Type: proto.MsgAttach}); err != nil {
		return "", err
	}
	if _, err := tea.NewProgram(m).Run(); err != nil {
		return "", err
	}
	return m.switchTo, nil
}

// spawnServer re-execs this binary as a detached daemon for the named
// session.
func spawnServer(session string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	sock, err := proto.SocketPath(session)
	if err != nil {
		return err
	}
	log, err := os.OpenFile(filepath.Join(filepath.Dir(sock), "server.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer log.Close()

	c := exec.Command(exe, "__server", session)
	c.Stdin = nil
	c.Stdout, c.Stderr = log, log
	c.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return c.Start()
}

// killServer stops one session's daemon and every shell in it, or — given no
// "-t" — every running session's. Either way it kills shells with no chance
// to save work, so it asks first rather than trusting that typing the
// command was already saying so.
func killServer(session string, explicit bool) error {
	if explicit {
		if !confirm(fmt.Sprintf("kill session %q and every shell in it?", sessionOrDefault(session))) {
			return nil
		}
		return killOneServer(session)
	}

	names, err := proto.Sessions()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return fmt.Errorf("no server running")
	}
	if !confirm(fmt.Sprintf("kill all %d session(s) and every shell in them?", len(names))) {
		return nil
	}
	for _, n := range names {
		if err := killOneServer(n); err != nil {
			fmt.Fprintf(os.Stderr, "tile: session %q: %v\n", n, err)
		}
	}
	return nil
}

func sessionOrDefault(session string) string {
	if session == "" {
		return "default"
	}
	return session
}

func killOneServer(session string) error {
	sock, err := proto.SocketPath(session)
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

// confirm asks a yes/no question on the terminal, defaulting to no on
// anything but an explicit "y".
func confirm(question string) bool {
	fmt.Fprintf(os.Stderr, "%s [y/N] ", question)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.ToLower(strings.TrimSpace(line))
	return line == "y" || line == "yes"
}

// listSessions prints the name of every session with a server currently
// running.
func listSessions() error {
	names, err := proto.Sessions()
	if err != nil {
		return err
	}
	if len(names) == 0 {
		fmt.Println("no sessions")
		return nil
	}
	for _, n := range names {
		fmt.Println(n)
	}
	return nil
}
