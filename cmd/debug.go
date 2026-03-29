package cmd

import (
	"fmt"
	"os"
)

func debugf(format string, args ...any) {
	if os.Getenv("QUANCODE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[quancode:debug] "+format+"\n", args...)
	}
}
