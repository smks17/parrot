package engine

import (
	"encoding/json"

	"parrot/internal/engine/shell"
	"parrot/internal/engine/vfs"
)

type snapshot struct {
	Root    *vfs.DumpNode     `json:"root"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	History []string          `json:"history"`
}

func (s *Session) Snapshot() ([]byte, error) {
	return json.Marshal(snapshot{
		Root:    s.fs.RootNode().Dump(),
		Cwd:     s.fs.Cwd(),
		Env:     s.shell.Vars(),
		History: s.shell.History,
	})
}

func LoadSession(data []byte) (*Session, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	filesystem := vfs.FromRoot(snap.Root.ToNode(), snap.Cwd)
	session := &Session{fs: filesystem, shell: shell.New(filesystem)}
	if snap.Env != nil {
		session.shell.SetVars(snap.Env)
	}
	session.shell.History = append([]string(nil), snap.History...)
	return session, nil
}
