package commands

import (
	"fmt"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"parrot/internal/engine/clock"
	"parrot/internal/engine/filesystem"
)

type Ls struct{}

var _ Command = Ls{}

func (ls Ls) Name() string {
	return "ls"
}

func (ls Ls) Usage() string {
	return "ls [-l] [-a] [-i] [path]  — list directory contents"
}

func (ls Ls) Run(ctx *Context, args []string) int {
	var all, long, inum bool
	var operands []string
	for _, arg := range args {
		if len(arg) > 1 && arg[0] == '-' {
			for _, flag := range arg[1:] {
				switch flag {
				case 'a':
					all = true
				case 'l':
					long = true
				case 'i':
					inum = true
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

	entries, err := ctx.VFS.List(path)
	if err != nil {
		fmt.Fprintf(ctx.Stderr, "%s: %s: %v\n", ls.Name(), path, err)
		return 1
	}

	rows := make([]lsRow, 0, len(entries)+2)
	// "." and ".." are real entries, so -a shows them the way it shows any
	// other name rather than by inventing two rows.
	if all {
		if dir, err := ctx.VFS.Stat(path); err == nil && dir.IsDir() {
			rows = append(rows, lsRow{info: dir, name: "."})
			if parent, err := ctx.VFS.Stat(path + "/.."); err == nil {
				rows = append(rows, lsRow{info: parent, name: ".."})
			}
		}
	}
	for _, entry := range entries {
		if !all && strings.HasPrefix(entry.Name, ".") {
			continue
		}
		rows = append(rows, lsRow{info: entry, name: entry.Name})
	}

	if !long {
		for _, row := range rows {
			if inum {
				fmt.Fprintf(ctx.Stdout, "%d %s\n", row.info.Ino, row.name)
				continue
			}
			fmt.Fprintln(ctx.Stdout, row.name)
		}
		return 0
	}

	width := 0
	for _, row := range rows {
		if l := len(row.size()); l > width {
			width = l
		}
	}

	tw := tabwriter.NewWriter(ctx.Stdout, 0, 4, 1, ' ', 0)
	now := clock.Now()
	for _, row := range rows {
		if inum {
			fmt.Fprintf(tw, "%d\t", row.info.Ino)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			row.info.Mode, row.links(), row.info.Owner.Name, row.info.Owner.Primary(),
			fmt.Sprintf("%*s", width, row.size()), row.modTime(now), row.name)
	}
	tw.Flush()
	return 0
}

type lsRow struct {
	info filesystem.Info
	name string
}

func (r lsRow) links() string { return strconv.Itoa(r.info.Links) }

func (r lsRow) modTime(now time.Time) string {
	t := r.info.ModTime.In(now.Location())
	if t.After(now.AddDate(0, -6, 0)) && t.Before(now.Add(time.Hour)) {
		return t.Format("Jan _2 15:04")
	}
	return t.Format("Jan _2  2006")
}

func (r lsRow) size() string {
	if r.info.IsDir() {
		return "-"
	}
	return strconv.FormatInt(r.info.Size, 10)
}

func init() { Register(Ls{}) }
