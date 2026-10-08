package proc

import (
	"context"
	"slices"
)

// Group is a set of processes that belong together
type Group struct {
	table   *Table
	pgid    PID        // the leader's PID; 0 until a process joins
	members []*Process // the ones still running; a process leaves when it ends
}

// NewGroup makes an empty group. The first process started in it leads it.
func (t *Table) NewGroup() *Group { return &Group{table: t} }

// Spawn starts a process in the group.
func (g *Group) Spawn(spec Spec) *Process {
	spec.Group = g
	return g.table.Spawn(spec)
}

// PGID is the group's process group, 0 before its first process.
func (g *Group) PGID() PID {
	g.table.mu.Lock()
	defer g.table.mu.Unlock()
	return g.pgid
}

// List is the group's processes still running, in the order they started.
func (g *Group) List() []Info {
	t := g.table
	t.mu.Lock()
	defer t.mu.Unlock()
	infos := make([]Info, 0, len(g.members))
	for _, p := range g.members {
		infos = append(infos, p.infoLocked())
	}
	return infos
}

// Kill sends sig to every process in the group.
func (g *Group) Kill(sender *Process, sig Signal) error {
	pgid := g.PGID()
	if pgid == 0 {
		return ErrNoProcess
	}
	return g.table.KillGroup(sender, pgid, sig)
}

// Wait waits until no process in the group is running, or with stops, until
// every one still alive is stopped; it reports whether that is why it
// returned.
func (g *Group) Wait(stops bool) (stopped bool) {
	t := g.table
	t.mu.Lock()
	defer t.mu.Unlock()
	t.waitUntilLocked(context.Background(), func() bool {
		return len(g.members) == 0 || stops && stoppedLocked(g.members)
	})
	return len(g.members) > 0
}

func (g *Group) WaitStopped(ctx context.Context) bool {
	t := g.table
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.waitUntilLocked(ctx, func() bool { return stoppedLocked(g.members) }) == nil
}

// joinLocked adds p to the group; the first to join leads it.
func (g *Group) joinLocked(p *Process) {
	if g.pgid == 0 {
		g.pgid = p.PID
	}
	if g.table.groups[g.pgid] == nil {
		g.table.groups[g.pgid] = g
	}
	p.PGID, p.group = g.pgid, g
	g.members = append(g.members, p)
}

// leaveLocked takes p out of its group, once it has ended. An empty group
// is gone from the table, as on Linux: nothing can be sent to it any more.
func (g *Group) leaveLocked(p *Process) {
	g.members = slices.DeleteFunc(g.members, func(q *Process) bool { return q == p })
	if len(g.members) == 0 && g.table.groups[g.pgid] == g {
		delete(g.table.groups, g.pgid)
	}
}
