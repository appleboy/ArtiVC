/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"fmt"

	"github.com/infuseai/art/internal/core"
	"github.com/spf13/cobra"
)

// getCmd represents the download command
var pullCmd = &cobra.Command{
	Use:   "pull",
	Short: "Switch workspace to the specific version",
	Long: `Switch workspace to the specific version. For example:

# switch to the latest version
art pull

# switch to the specific version
art pull v1.0.0
`,
	Run: pull,
}

func pull(cmd *cobra.Command, args []string) {

	config, err := core.LoadConfig("")
	if err != nil {
		fmt.Printf("pull %v \n", err)
		return
	}

	mngr, err := core.NewArtifactManager(config)
	if err != nil {
		fmt.Printf("pull %v \n", err)
		return
	}

	err = mngr.Pull(core.PullOptions{Fetch: true})
	if err != nil {
		fmt.Printf("pull %v \n", err)
	}
}

func init() {
	rootCmd.AddCommand(pullCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// downloadCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// downloadCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
