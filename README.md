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
- `cmd/web/` — the WebAssembly page any demo can be built into. See
  [Running in a browser](#running-in-a-browser).

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

## Running in a browser

```
bash cmd/web/build.sh pbr
python -m http.server 8731 --bind 127.0.0.1 --directory cmd/web
# then open http://127.0.0.1:8731/
```

`build.sh` takes any directory under `cmd/scene/` and defaults to `pbr`. It
writes four generated files into `cmd/web/`, all gitignored: `main.wasm`, the
`assets.tar.gz` bundle, `demo.js` (the demo's name, for the page title) and a
copy of the Go runtime's `wasm_exec.js`. Serve the directory with anything that
sends `Content-Type: application/wasm` for `.wasm` — Python's `http.server`
does — and open it in a WebGPU-capable browser: Chrome or Edge 113+, Safari 18+,
or Firefox with WebGPU enabled.

**No demo carries a line of code about the browser.** The whole of the platform
difference is `internal/assets.Config`: on disk it walks up to the checkout's
`assets/` directory, and under `GOOS=js` it takes the map `index.html` unpacked
out of the tar before the module booted. Both land on the `assets` mount and
answer the same `assets/Fox/Fox.glb` paths, so a demo that runs on the desktop
runs in a browser by being named to `build.sh`.

**Why bother, when desktop is the acceptance bar.** A native device reports
*hardware* limits, which sit far above the WebGPU floor, so a desktop run
provably cannot catch a web limit violation — and scene's storage-buffer budget
has no spare, where a single unbound binding kills the whole frame with nothing
logged. One demo has to actually run in a browser for that to be checked at all.

**Two things that will otherwise read as bugs.** gogpu binds `keydown` on the
canvas element rather than on the document, so `index.html` gives the canvas a
`tabindex` and focuses it — without that a demo's hotkeys silently do nothing.
And `navigator.gpu` is undefined on `about:blank` even in a browser where WebGPU
works, so probe it on the served page and not on a blank tab.

`go test ./cmd/web` cross-compiles every directory under `cmd/scene` for
`GOOS=js` and is the guard on all of that. The failure it catches — an import
that only exists on the desktop, or a call that assumed a filesystem — costs a
desktop run nothing and is otherwise found only the next time someone opens a
browser.

To drive it headlessly — for a screenshot, or to check a change reaches the
frame — Chrome needs `--enable-unsafe-webgpu --use-webgpu-adapter=swiftshader
--enable-features=Vulkan`. `--use-webgpu-adapter=swiftshader` is the one that
matters: without it the demo logs `wgpu: no suitable GPU adapter found` and
stops. Wait for `document.getElementById('status').className === 'hidden'`, then
a few seconds for the first frame.

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
