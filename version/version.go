package version

// Version is set at build time via ldflags. Development builds show "dev".
//
//	go build -ldflags "-X github.com/qq418716640/quancode/version.Version=v0.1.0"
var Version = "dev"
