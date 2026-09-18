package engine

import (
	"encoding/json"
	"time"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/commands"
	"parrot/internal/engine/shell"
	"parrot/internal/engine/user"
	"parrot/internal/engine/vfs"
)

type snapshot struct {
	Root    *vfs.DumpNode     `json:"root"`
	Cwd     string            `json:"cwd"`
	Env     map[string]string `json:"env"`
	History []historyRecord   `json:"history"`
	User    string            `json:"user,omitempty"`
	// Clock is how far `date -s` moved the shell's clock, in nanoseconds, and
	// Zone/ZoneOffset is the host zone it was saved in — a shared session
	// shows the same times to whoever opens it. TZ rides along in Env.
	Clock      int64  `json:"clock_offset,omitempty"`
	Zone       string `json:"zone,omitempty"`
	ZoneOffset int    `json:"zone_offset,omitempty"`
}

type historyRecord struct {
	Line string `json:"line"`
	At   int64  `json:"at,omitempty"`
}

func (h *historyRecord) UnmarshalJSON(data []byte) error {
	var line string
	if err := json.Unmarshal(data, &line); err == nil {
		h.Line, h.At = line, 0
		return nil
	}
	type record historyRecord
	var r record
	if err := json.Unmarshal(data, &r); err != nil {
		return err
	}
	*h = historyRecord(r)
	return nil
}

func (s *Session) Snapshot() ([]byte, error) {
	history := make([]historyRecord, len(s.shell.History))
	for i, entry := range s.shell.History {
		history[i] = historyRecord{Line: entry.Line}
		if !entry.At.IsZero() {
			history[i].At = entry.At.Unix()
		}
	}
	snap := snapshot{
		Root:    s.fs.RootNode().Dump(),
		Cwd:     s.fs.Cwd(),
		Env:     s.shell.Vars(),
		History: history,
		User:    s.User(),
		Clock:   int64(clock.Offset()),
	}
	snap.Zone, snap.ZoneOffset = clock.Local()
	return json.Marshal(snap)
}

func LoadSession(data []byte) (*Session, error) {
	var snap snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, err
	}

	clock.SetOffset(time.Duration(snap.Clock))
	if snap.Zone != "" {
		clock.SetLocal(snap.Zone, snap.ZoneOffset)
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
	history := make([]commands.HistoryEntry, len(snap.History))
	for i, record := range snap.History {
		history[i] = commands.HistoryEntry{Line: record.Line}
		if record.At != 0 {
			history[i].At = time.Unix(record.At, 0)
		}
	}
	session.shell.History = append([]commands.HistoryEntry(nil), history...)
	return session, nil
}
