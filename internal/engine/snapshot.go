package engine

import (
	"encoding/json"
	"parrot/internal/engine/vfs"
)

type snapshot struct {
	Root    *vfs.DumpNode     `json:"root"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	History []string          `json:"history"`
	User    string            `json:"user,omitempty"`
	Group   string            `json:"group,omitempty"`
}

func (s *Session) Snapshot() ([]byte, error) {
	snap := snapshot{
		Root:    s.vfs.RootNode().Dump(),
		Cwd:     s.vfs.Cwd(),
		Env:     s.env,
		History: s.history,
		User:    s.user,
		Group:   s.group,
	}
	return json.Marshal(snap)
}

func LoadSession(data []byte) (*Session, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	env := make(map[string]string, len(snap.Env))
	for k, v := range snap.Env {
		env[k] = v
	}

	user, group := snap.User, snap.Group
	if user == "" {
		user = vfs.HomeUser
	}
	if group == "" {
		group = vfs.HomeGroup
	}

	v := vfs.FromRoot(snap.Root.ToNode(), snap.Cwd)
	v.SetUser(user, group)

	return &Session{
		vfs:     v,
		env:     env,
		history: append([]string(nil), snap.History...),
		user:    user,
		group:   group,
	}, nil
}
