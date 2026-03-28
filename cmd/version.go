package cmd

import (
	"fmt"

	quancodeversion "github.com/qq418716640/quancode/version"
	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the quancode version",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println(quancodeversion.Version)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
