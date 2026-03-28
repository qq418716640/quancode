package version

// Version is the default development version. Release builds can override it
// with ldflags, for example:
//
//	go build -ldflags "-X github.com/qq418716640/quancode/version.Version=v0.1.0-alpha"
var Version = "v0.1.0-alpha"
