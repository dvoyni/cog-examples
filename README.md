# cog-examples

Runnable usage examples for the [cog](https://github.com/dvoyni/cog) engine.
The module exists so engine work has somewhere to land a small, throwaway app
that exercises a contract end to end, without dragging a real game into it. The
examples are collected here for later publication alongside the engine.

## Layout

- `cmd/scene/<demo>/main.go` — one `main.go` per scene-plugin demo. Each demo is
  self-contained: it wires its own plugin list and holds its own gameplay
  plugin in the same file.

## Building against a local cog

`go.mod` carries `replace github.com/dvoyni/cog => ../cog`, so the module builds
against the sibling checkout rather than a tagged release. Clone it next to
`cog`:

```
<parent>/
  cog/
  cog-examples/
```

That is deliberate: the demos exercise unreleased `gfx` and `scene` changes, so
they must compile against the working tree.

## Running

```
go run ./cmd/scene/hello
```

`hello` opens a window through `storage`, `input`, `gfx`, `canvas` and `wgpu`
and draws one canvas rectangle on a dark background. It proves the module builds
and runs against the sibling `cog`; every other demo starts from its wiring.
