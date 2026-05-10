# Claude Notes

## README

- Automatically update README.md when adding new functionality or changing existing behaviour. Where appropriate, ensure the changes are reflected in the application help screen, and also the in-app help (`ctrl+H`). Update the relevant section as part of the same task before considering the work done.

## Build & Test

```
go build -o ftail .
go test -v ./...
```

## Architecture

Single `main` package - do not introduce sub-packages.

Key design points:

- Bubble Tea single-threaded event loop; file tailing uses polling, not inotify
- `entries` is a circular buffer; `filtered` is a separate index slice over it
- Persistent query history is stored at `~/.ftailhst` (up to 100 entries)

## Coding Conventions

- Table-driven tests with `t.Run()` in `*_test.go` files alongside source files
- Silent error handling for non-critical paths (history file, update checks)
- No linter config - follow standard Go conventions

## Releases

- Update `CHANGELOG.md` for every feature addition or bug fix
- Version is set via ldflags at build time from the git tag; do not hardcode it in source
