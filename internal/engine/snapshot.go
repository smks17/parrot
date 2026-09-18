package engine

import (
	"encoding/json"

	"parrot/internal/engine/shell"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
)

type snapshot struct {
	Root    *vfs.DumpNode     `json:"root"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	History []string          `json:"history"`
	User    string            `json:"user,omitempty"`
}

func (s *Session) Snapshot() ([]byte, error) {
	snap := snapshot{
		Root:    s.fs.RootNode().Dump(),
		Cwd:     s.fs.Cwd(),
		Env:     s.shell.Vars(),
		History: s.shell.History,
		User:    s.User(),
	}
	return json.Marshal(snap)
}

func LoadSession(data []byte) (*Session, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	filesystem := vfs.FromRoot(snap.Root.ToNode(), snap.Cwd)
	session := &Session{fs: filesystem, shell: shell.New(filesystem, nil)}
	session.shell.SetUser = session.SetUser
	session.users = user.NewDB(filesystem)

	// A saved user who no longer exists — the account was deleted, or
	// /etc/passwd was removed before saving
	if snap.User == "" || session.SetUser(snap.User) != nil {
		session.SetUser(vfs.HomeUser)
	}

	if snap.Env != nil {
		session.shell.SetVars(snap.Env)
	}
	session.shell.History = append([]string(nil), snap.History...)
	return session, nil
}
