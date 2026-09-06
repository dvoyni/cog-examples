#!/usr/bin/env bash
# Build one scene demo into this directory as a WebAssembly page: main.wasm,
# assets.tar.gz, demo.js, and a copy of the Go runtime's wasm_exec.js. Serve this
# directory with any static server and open index.html in a WebGPU-capable
# browser (Chrome/Edge 113+, Safari 18+, or Firefox with WebGPU enabled).
#
#   Usage: ./build.sh [demo]        # default: pbr
#          ./build.sh box
#
# Any directory under cmd/scene/ works, and no demo carries a line of code about
# the browser: internal/assets swaps its mount under GOOS=js for the bundle
# index.html unpacks, and every demo already reaches the asset set through it.
#
# This exists because a desktop run provably cannot catch a web limit violation.
# A native device reports hardware limits, which are far above the WebGPU floor;
# the storage-buffer budget has no spare, and a single unbound binding kills the
# whole frame silently. So one demo has to actually run in a browser.
set -euo pipefail

demo="${1:-pbr}"

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$here/../.." && pwd)"

if [[ ! -d "$repo_root/cmd/scene/$demo" ]]; then
	echo "no such demo: cmd/scene/$demo" >&2
	echo "available:" >&2
	(cd "$repo_root/cmd/scene" && ls -d */ | sed 's#/##; s#^#  #') >&2
	exit 1
fi

echo "› building main.wasm from cmd/scene/$demo (GOOS=js GOARCH=wasm CGO_ENABLED=0)…"
GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -C "$repo_root" \
	-trimpath -buildvcs=false -ldflags="-s -w" -o "$here/main.wasm" "./cmd/scene/$demo"

# The whole assets/ directory, tarred from the repository root so its entries
# carry the assets/ prefix a demo names a model by. ATTRIBUTION.md goes with it,
# and not as a courtesy: six of the vendored assets are CC-BY-4.0, whose
# attribution condition binds every copy a recipient gets, and a wasm bundle is
# a copy without the repository.
echo "› packing assets.tar.gz…"
rm -f "$here/assets.tar.gz"
tar -czf "$here/assets.tar.gz" -C "$repo_root" assets

# The demo's name, for the page title. Nothing else in the page changes between
# demos, and wgpu.Config.WithTitle is a desktop window title that no browser
# ever sees.
echo "› writing demo.js…"
printf 'globalThis.__cogDemo = "%s";\n' "$demo" > "$here/demo.js"

echo "› copying wasm_exec.js from GOROOT…"
rm -f "$here/wasm_exec.js"
install -m 0644 "$(go env GOROOT)/lib/wasm/wasm_exec.js" "$here/wasm_exec.js"

cat <<EOF
› done. Serve this directory and open index.html in a WebGPU browser, e.g.

    python -m http.server 8731 --bind 127.0.0.1 --directory "$here"
    # then open http://127.0.0.1:8731/
EOF
