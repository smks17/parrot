# Parrot

A virtual Linux environment that runs in the browser and CLI. It is a simulation, not a real operating system: the shell, filesystem, users, pipes and processes are all written in Go.


## What it has

- A shell with pipelines, redirection, `if`/loops/`for`, functions, `source`, globbing and job control (`&`, `jobs`, `fg`, `bg`, Ctrl-Z, Ctrl-C).
- An in-memory filesystem with owners, groups, permission bits, hard links and `/dev` devices.
- Users and login (`/etc/passwd`, `/etc/shadow`, `su`).
- A process table with PIDs, signals and `ps`/`kill`.
- About 20 commands: `cat`, `cd`, `chmod`, `chown`, `cp`, `cut`, `date`, `echo`, `ls`, `mkdir`, `mv`, `rm`, `wc`, and more. Type `help` to list them.
- Session save and restore, with an optional Cloudflare Worker backend to store sessions.
- Network feature

## Try it

You need Go 1.25 or newer.

```sh
go run ./cmd/prt                 # an in-memory machine
go run ./cmd/prt -real           # the same shell on your real disk
go run ./cmd/prt script.sh a b   # run a script with arguments
go run ./cmd/prt --help          # see usage
```

### In the browser

You also need Node 22 or newer.

```sh
GOOS=js GOARCH=wasm go build -o web/public/engine.wasm ./cmd/wasm
cd web
npm install
npm run dev
```

`./deploy.sh` builds the engine, then deploys the Worker in `backend/` and the static site to Cloudflare Pages. It needs `npx wrangler login` first.

## Check your changes

```sh
go build ./... && go vet ./... && go test ./...
GOOS=js GOARCH=wasm go build -o /dev/null ./cmd/wasm   # the engine must still compile to WASM
gofmt -l .                                             # must print nothing
```

## How it is organised

```text
web/          Next.js terminal (presentation only)
cmd/wasm      thin browser adapter
cmd/prt       native command line
internal/engine/
  shell/      lexer, parser, executor, jobs
  commands/   one file per command
  filesystem/ filesystem interface
  vfs/        in-memory filesystem
  ofs/        real disk filesystem
  stream/     files, descriptors, pipes
  proc/       process table and signals
  user/       accounts and passwords
  network/    network and dns
backend/      Cloudflare Worker that stores sessions
```

The rule that shapes everything: the engine must never use the host operating system to implement a feature, and it must always compile to WebAssembly.

## TODO
- [ ] Add command snapshot inside cli like browser
- [ ] Run wasm program inside the parrot
- [ ] Add package and plugin manager
- [ ] Complete remain engine features inside frontend
- [ ] Rewrite backend with Python or Go
- [ ] Unittest
- [ ] Implement mount
- [ ] Implement systemd
- [ ] Resource model (memory, CPU), symlinks, atomic Move.


## License

See [LICENSE](LICENSE).
