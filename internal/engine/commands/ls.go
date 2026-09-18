package commands

import (
	"fmt"
	"parrot/internal/engine/vfs"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
)

type Ls struct{}

var _ Command = Ls{}

func (ls Ls) Name() string {
	return "ls"
}

func (ls Ls) Usage() string { return "ls [-l] [-a] [path]  — list directory contents" }

func (ls Ls) Run(ctx *Context, args []string) int {
	var all, long bool
	var operands []string
	for _, arg := range args {
		if len(arg) > 1 && arg[0] == '-' {
			for _, flag := range arg[1:] {
				switch flag {
				case 'a':
					all = true
				case 'l':
					long = true
				default:
					fmt.Fprintf(ctx.Stderr, "%s: invalid option -- '%c'\n", ls.Name(), flag)
					return 1
				}
			}
			continue
		}
		operands = append(operands, arg)
	}

	path := ctx.VFS.Cwd()
	if len(operands) > 0 {
		path = operands[0]
	}

	nodes, err := ctx.VFS.List(path)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", ls.Name(), path, err)
		return 1
	}

	if !long {
		for _, node := range nodes {
			if !all && strings.HasPrefix(node.Name, ".") {
				continue
			}
			name := node.Name
			fmt.Fprintln(ctx.Stdout, name)
		}
		return 0
	}

	rows := make([]lsRow, 0, len(nodes)+2)
	if all {
		if dir, err := ctx.VFS.Resolve(path); err == nil && dir.IsDir() {
			rows = append(rows, lsRow{node: dir, name: "."})
			if dir.Parent != nil {
				rows = append(rows, lsRow{node: dir.Parent, name: ".."})
			}
		}
	}
	for _, node := range nodes {
		if !all && strings.HasPrefix(node.Name, ".") {
			continue
		}
		rows = append(rows, lsRow{node: node, name: node.Name})
	}

	width := 0
	for _, row := range rows {
		if l := len(row.size()); l > width {
			width = l
		}
	}

	tw := tabwriter.NewWriter(ctx.Stdout, 0, 4, 1, ' ', 0)
	now := now(ctx)
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.node.Mode, row.links(), row.node.Owner, row.node.Group,
			fmt.Sprintf("%*s", width, row.size()), row.modTime(now), row.name)
	}
	tw.Flush()
	return 0
}

type lsRow struct {
	node *vfs.Node
	name string
}

func (r lsRow) links() string {
	if r.node.IsDir() {
		n := 2
		for _, child := range r.node.Children {
			if child.IsDir() {
				n++
			}
		}
		return strconv.Itoa(n)
	}
	return "1"
}

func (r lsRow) modTime(now time.Time) string {
	t := r.node.ModTime.In(now.Location())
	if t.After(now.AddDate(0, -6, 0)) && t.Before(now.Add(time.Hour)) {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}

func (r lsRow) size() string {
	if r.node.IsDir() {
		return "-"
	}
	return strconv.Itoa(len(r.node.Content))
}

func init() { Register(Ls{}) }
