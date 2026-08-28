package main

import (
	"encoding/json"
	"net"

	tea "charm.land/bubbletea/v2"

	"tile/internal/proto"
)

// clientModel is the whole client: it forwards input to the server and paints
// the frames that come back. None of the multiplexer's state lives here, which
// is what lets the session outlive the client on detach.
type clientModel struct {
	enc   *json.Encoder
	dec   *json.Decoder
	frame string
	cur   *tea.Cursor

	// switchTo is set by a MsgDetach that names a session to attach to
	// next, sent by the sessions picker (s p) instead of a plain detach.
	// attach()'s caller checks it once Run returns to decide whether to
	// stop there or dial the next session.
	switchTo string
}

type connClosedMsg struct{}

func newClient(conn net.Conn) *clientModel {
	return &clientModel{enc: json.NewEncoder(conn), dec: json.NewDecoder(conn)}
}

func (m *clientModel) Init() tea.Cmd { return m.recv }

// recv blocks on the socket and delivers the next server message as a Msg.
func (m *clientModel) recv() tea.Msg {
	var sm proto.ServerMsg
	if err := m.dec.Decode(&sm); err != nil {
		return connClosedMsg{}
	}
	return sm
}

// send writes a message to the server, e.g. from outside the bubbletea loop.
func (m *clientModel) send(c proto.ClientMsg) error {
	return m.enc.Encode(c)
}

func (m *clientModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case proto.ServerMsg:
		switch msg.Type {
		case proto.MsgFrame:
			m.frame = msg.Content
			m.cur = nil
			if msg.CurVis {
				m.cur = tea.NewCursor(msg.CurX, msg.CurY)
			}
		case proto.MsgDetach:
			m.switchTo = msg.Content
			return m, tea.Quit
		case proto.MsgClipboard:
			return m, tea.Batch(m.recv, tea.SetClipboard(msg.Content))
		}
		return m, m.recv

	case connClosedMsg:
		return m, tea.Quit

	case tea.WindowSizeMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgResize, W: msg.Width, H: msg.Height})

	case tea.KeyPressMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgKey, Key: tea.Key(msg)})

	case tea.PasteMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgPaste, Text: msg.Content})

	case tea.MouseClickMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseClick})
	case tea.MouseReleaseMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseRelease})
	case tea.MouseMotionMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseMotion})
	case tea.MouseWheelMsg:
		_ = m.send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseWheel})
	}
	return m, nil
}

func (m *clientModel) View() tea.View {
	return tea.View{
		Content:   m.frame,
		Cursor:    m.cur,
		AltScreen: true,
		MouseMode: tea.MouseModeCellMotion,
	}
}
