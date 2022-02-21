/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>

*/
package cmd

import (
	"os"

	"github.com/infuseai/art/internal/core"
	"github.com/spf13/cobra"
)

var initCommand = &cobra.Command{
	Use:   "init",
	Short: "Initiate a workspace",
	Example: `  # Init a workspace with local repo
  art init /path/to/mydataset

  # Init a workspace with s3 repo
  art init s3://mybucket/path/to/mydataset`,
	Args: cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		repo := args[0]
		core.InitWorkspace(cwd, repo)
	},
}

func init() {
}
