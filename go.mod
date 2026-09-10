module github.com/dvoyni/cog-examples

go 1.27

require (
	github.com/dvoyni/cog v0.0.0-00010101000000-000000000000
	github.com/gogpu/naga v0.19.0
	github.com/qmuntal/gltf v0.29.0
	golang.org/x/image v0.44.0
)

require (
	github.com/go-webgpu/goffi v0.6.3 // indirect
	github.com/go-webgpu/webgpu v0.5.5 // indirect
	github.com/gogpu/gogpu v0.54.0 // indirect
	github.com/gogpu/gpucontext v0.31.3 // indirect
	github.com/gogpu/gputypes v0.8.0 // indirect
	github.com/gogpu/wgpu v0.34.5 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
)

replace github.com/dvoyni/cog => ../cog

// The same override cog carries in its own go.mod (4ed0b85). A replace
// directive in a dependency does not apply to the main module, so without this
// line every demo here compiles against a naga whose SPIR-V backend drops a
// module-scope vector used as an operand and hands the shader (0, 0, 0) - which
// zeroes SCENE_DIELECTRIC_F0 in the bundled PBR on every Vulkan machine, and
// zeroed the frame ladder's METAL_F0 too. See dvoyni/cog#181, gogpu/naga#92.
// Comes back out once a fixed naga is released.
replace github.com/gogpu/naga => github.com/dvoyni/naga v0.19.1-0.20260909205556-fee6c529ac74
