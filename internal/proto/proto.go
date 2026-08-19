// Package proto is the wire contract between the yatm client and server:
// newline-delimited JSON over a per-user unix socket.
package proto

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
)

// tea.Key and tea.Mouse are plain exported structs, so they travel as-is.

const (
	MsgKey    = "key"
	MsgMouse  = "mouse"
	MsgResize = "resize"
	MsgDetach = "detach"
	MsgKill   = "kill"
	MsgFrame  = "frame"
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
}

// ServerMsg travels from the server to the attached client.
type ServerMsg struct {
	Type    string `json:"t"`
	Content string `json:"c,omitempty"`
	CurX    int    `json:"x,omitempty"`
	CurY    int    `json:"y,omitempty"`
	CurVis  bool   `json:"v,omitempty"`
}

// SocketPath returns the per-user socket both sides dial, creating its
// directory.
// ponytail: one session per socket. Named sessions are a flag on this path
// plus a lookup, once more than one is worth having.
func SocketPath() (string, error) {
	dir := filepath.Join(os.TempDir(), fmt.Sprintf("yatm-%d", os.Getuid()))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "default.sock"), nil
}
