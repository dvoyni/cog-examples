# cog-examples

Runnable usage examples for the [cog](https://github.com/dvoyni/cog) engine.
The module exists so engine work has somewhere to land a small, throwaway app
that exercises a contract end to end, without dragging a real game into it. The
examples are collected here for later publication alongside the engine.

## Layout

- `cmd/scene/<demo>/main.go` — one `main.go` per scene-plugin demo. Each demo is
  self-contained: it wires its own plugin list and holds its own gameplay
  plugin in the same file.
- `assets/` — the vendored demo models, one `.glb` per asset, plus
  [`ATTRIBUTION.md`](assets/ATTRIBUTION.md).
- `cmd/prepare-assets/` — the tool that builds `assets/`, and the manifest that
  says where each model comes from and what its licence obliges.
- `internal/assets` — mounts `assets/` through `storage`.
- `internal/headless` — runs a cog engine with no GPU, so a demo's assertions
  can be a plain `go test` beside its `main.go`.

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

## Assets

The models the demos load are committed under `assets/`, one self-contained
`.glb` per asset, and every one of them is a modified copy of something from the
Khronos [glTF Sample Assets](https://github.com/KhronosGroup/glTF-Sample-Assets)
repository. What each one is, where it came from and what its licence obliges is
in [`assets/ATTRIBUTION.md`](assets/ATTRIBUTION.md) — which is generated, along
with the assets themselves, by:

```
go run ./cmd/prepare-assets
```

You do not need to run it: its output is committed. Run it after editing the
manifest in that command, or to check that upstream has not relicensed anything
underneath the set — it verifies that before it writes a byte, and stops if the
terms have moved.

A demo reaches the set through `internal/assets`, because
`storage.DefaultConfig` alone does not: its default read mount is the
executable's own directory, and `go run` builds into a temporary one.

```go
config, err := assets.Config(storage.DefaultConfig("cog-examples"))
```

Paths keep the repository's spelling — `assets/Fox/Fox.glb`.
