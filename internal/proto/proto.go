// Package proto is the wire contract between the tile client and server:
// newline-delimited JSON over a per-user unix socket.
package proto

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
)

// tea.Key and tea.Mouse are plain exported structs, so they travel as-is.

const (
	MsgKey    = "key"
	MsgPaste  = "paste"
	MsgMouse  = "mouse"
	MsgResize = "resize"
	MsgDetach = "detach"
	MsgKill   = "kill"
	MsgFrame  = "frame"

	// MsgAttach asks to become the attached client. Connecting is not enough:
	// a one-shot CLI command dials the same socket and must not displace
	// whoever is attached.
	MsgAttach = "attach"
	// MsgCmd is a one-shot CLI command; MsgReply is its single answer.
	MsgCmd   = "cmd"
	MsgReply = "reply"
)

// Mouse event kinds, mirroring bubbletea's four mouse messages.
const (
	MouseClick   = "click"
	MouseRelease = "release"
	MouseMotion  = "motion"
	MouseWheel   = "wheel"
)

// ClientMsg travels from the attached client to the server.
type ClientMsg struct {
	Type  string    `json:"t"`
	Key   tea.Key   `json:"k,omitzero"`
	Mouse tea.Mouse `json:"m,omitzero"`
	Kind  string    `json:"mk,omitempty"`
	W     int       `json:"w,omitempty"`
	H     int       `json:"h,omitempty"`

	// MsgPaste only: the pasted text, forwarded from a bracketed-paste
	// event the client's terminal reported.
	Text string `json:"txt,omitempty"`

	// MsgCmd only: the subcommand and its raw arguments, forwarded verbatim
	// from the command line. The server owns all parsing, so there is one
	// place to add a command.
	Cmd  string   `json:"cmd,omitempty"`
	Args []string `json:"args,omitempty"`
}

const (
	// MsgClipboard asks the client to set the system clipboard (via OSC52)
	// to Content: the text mouse-selected out of a pane whose program isn't
	// itself reading the mouse.
	MsgClipboard = "clipboard"
)

// ServerMsg travels from the server to the attached client.
type ServerMsg struct {
	Type    string `json:"t"`
	Content string `json:"c,omitempty"`
	CurX    int    `json:"x,omitempty"`
	CurY    int    `json:"y,omitempty"`
	CurVis  bool   `json:"v,omitempty"`
	Err     string `json:"e,omitempty"` // MsgReply only: the command failed
}

// SessionDir is the per-user directory holding every session's socket,
// created if it doesn't exist yet.
func SessionDir() (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("tile-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// SocketPath returns the socket for the named session, creating the session
// directory. An empty name means the default session, so callers that don't
// care about multiple sessions can pass "".
func SocketPath(name string) (string, error) {
	dir, err := SessionDir()
	if err != nil {
		return "", err
	}
	if name == "" {
		name = "default"
	}
	return filepath.Join(dir, name+".sock"), nil
}

// Sessions lists the names of every session with a server currently
// listening, cleaning up any stale socket left behind by a server that died
// without removing it.
func Sessions() ([]string, error) {
	dir, err := SessionDir()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".sock")
		if !ok {
			continue
		}
		path := filepath.Join(dir, e.Name())
		conn, err := net.Dial("unix", path)
		if err != nil {
			os.Remove(path)
			continue
		}
		conn.Close()
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}
