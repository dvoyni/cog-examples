module github.com/dvoyni/cog-examples

go 1.27

require (
	github.com/dvoyni/cog v0.0.0-00010101000000-000000000000
	github.com/jfreymuth/oggvorbis v1.0.5
	github.com/qmuntal/gltf v0.29.0
	golang.org/x/image v0.44.0
)

require (
	github.com/ebitengine/oto/v3 v3.5.0 // indirect
	github.com/ebitengine/purego v0.11.0 // indirect
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/gogpu/gogpu v0.54.0 // indirect
	github.com/gogpu/gpucontext v0.31.3 // indirect
	github.com/gogpu/gputypes v0.8.0 // indirect
	github.com/gogpu/naga v0.19.0 // indirect
	github.com/gogpu/wgpu v0.34.5 // indirect
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/jfreymuth/pulse v0.1.3 // indirect
	github.com/jfreymuth/vorbis v1.0.2 // indirect
	github.com/modelcontextprotocol/go-sdk v1.7.0 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

replace github.com/dvoyni/cog => ../cog

// cog's own naga replace does not reach this module, so it is repeated here.
// Without it a module-scope vector const used as an operand reaches a SPIR-V
// shader as zero (dvoyni/cog#181, dvoyni/cog#185, gogpu/naga#92). Keep it on the
// version cog pins; it comes out when cog's does.
replace github.com/gogpu/naga => github.com/dvoyni/naga v0.19.1-0.20260910142728-7fd5ed312699

replace github.com/gogpu/gogpu => github.com/dvoyni/gogpu v0.54.1-0.20260910171621-041de1a5716f

// cog's vorbis pins, mirrored for the same reason: Go ignores a dependency's
// replace, so without these the build resolves the decoders against upstream
// and fails to compile once the forks add API (dvoyni/cog#511). Keep them on
// the versions cog pins; they come out when cog's do.
replace github.com/jfreymuth/vorbis => github.com/dvoyni/vorbis v1.0.3-0.20260921150454-ab3e6ce988a8

replace github.com/jfreymuth/oggvorbis => github.com/dvoyni/oggvorbis v1.0.6-0.20260921150857-546236badfd4

// cog's wgpu pin, mirrored for the same reason: Go ignores a dependency's
// replace, so without it the build resolves wgpu against upstream and loses the
// vertex arrayStride validation (dvoyni/cog#47). Keep it on the version cog
// pins; it comes out when cog's does.
replace github.com/gogpu/wgpu => github.com/dvoyni/wgpu v0.34.6-0.20260922155231-6ce612817da4
