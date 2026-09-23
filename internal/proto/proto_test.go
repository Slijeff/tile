package proto

import (
	"os"
	"testing"
)

// A pane's env holds its session's socket inode, fixed at spawn. Renaming
// the session renames the socket; the lookup must follow it to the new name.
func TestSessionByInoFollowsRename(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	old, _ := SocketPath("before")
	if err := os.WriteFile(old, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	ino, err := SocketIno(old)
	if err != nil {
		t.Fatal(err)
	}
	renamed, _ := SocketPath("after")
	if err := os.Rename(old, renamed); err != nil {
		t.Fatal(err)
	}
	if name, ok := SessionByIno(ino); !ok || name != "after" {
		t.Fatalf("SessionByIno = %q, %v; want \"after\", true", name, ok)
	}
}
