package pkginfo

var (
	// Version defines the pkg version.
	//
	// It's intended to be overwritten by the linker/compiler.
	//
	// See:
	//  - `-ldflags` https://pkg.go.dev/cmd/go#hdr-Compile_packages_and_dependencies
	//  - `-X` https://pkg.go.dev/cmd/link
	Version = "dev"
)
