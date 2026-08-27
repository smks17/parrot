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
}

func (s *Session) Snapshot() ([]byte, error) {
	snap := snapshot{
		Root:    s.vfs.RootNode().Dump(),
		Cwd:     s.vfs.Cwd(),
		Env:     s.env,
		History: s.history,
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

	return &Session{
		vfs:     vfs.FromRoot(snap.Root.ToNode(), snap.Cwd),
		env:     env,
		history: append([]string(nil), snap.History...),
	}, nil
}
