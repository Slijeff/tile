// Package client is the yatm client: it forwards input to the server and
// paints the frames that come back. None of the multiplexer's state lives
// here, which is what lets the session outlive the client on detach.
package client

import (
	"encoding/json"
	"net"

	tea "charm.land/bubbletea/v2"

	"yatm/internal/proto"
)

// Model is the whole client, implementing tea.Model.
type Model struct {
	enc   *json.Encoder
	dec   *json.Decoder
	frame string
	cur   *tea.Cursor
}

type connClosedMsg struct{}

func New(conn net.Conn) *Model {
	return &Model{enc: json.NewEncoder(conn), dec: json.NewDecoder(conn)}
}

func (m *Model) Init() tea.Cmd { return m.recv }

// recv blocks on the socket and delivers the next server message as a Msg.
func (m *Model) recv() tea.Msg {
	var sm proto.ServerMsg
	if err := m.dec.Decode(&sm); err != nil {
		return connClosedMsg{}
	}
	return sm
}

// Send writes a message to the server, e.g. from outside the bubbletea loop.
func (m *Model) Send(c proto.ClientMsg) error {
	return m.enc.Encode(c)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, tea.Quit
		}
		return m, m.recv

	case connClosedMsg:
		return m, tea.Quit

	case tea.WindowSizeMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgResize, W: msg.Width, H: msg.Height})

	case tea.KeyPressMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgKey, Key: tea.Key(msg)})

	case tea.MouseClickMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseClick})
	case tea.MouseReleaseMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseRelease})
	case tea.MouseMotionMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseMotion})
	case tea.MouseWheelMsg:
		_ = m.Send(proto.ClientMsg{Type: proto.MsgMouse, Mouse: tea.Mouse(msg), Kind: proto.MouseWheel})
	}
	return m, nil
}

func (m *Model) View() tea.View {
	return tea.View{
		Content:   m.frame,
		Cursor:    m.cur,
		AltScreen: true,
		MouseMode: tea.MouseModeCellMotion,
	}
}
