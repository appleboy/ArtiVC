package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/infuseai/artivc/internal/core"
	"github.com/infuseai/artivc/internal/repository"
	"github.com/spf13/cobra"
)

var configCommand = &cobra.Command{
	Use:                   "config [<key> [<value>]]",
	Short:                 "Configure the workspace",
	Long:                  "Configure the workspace. The config file is stored at \".avc/config\".",
	DisableFlagsInUseLine: true,
	Example: `  # List the config
  avc config

  # Get the config
  avc config repo.url

  # Set the config
  avc config repo.url s3://your-bucket/data`,
	Args: cobra.RangeArgs(0, 2),
	Run: func(cmd *cobra.Command, args []string) {
		config, err := core.LoadConfig("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return
		}

		switch len(args) {
		case 0:
			config.Print()
		case 1:
			value := config.Get(args[0])
			if value != nil {
				fmt.Println(value)
			} else {
				fmt.Fprintf(os.Stderr, "key not found: %s\n", args[0])
				os.Exit(1)
			}
		case 2:
			key := args[0]
			value := args[1]
			if key == "repo.url" {
				if strings.HasPrefix(value, "http") && !repository.IsAzureStorageUrl(value) {
					exitWithError(errors.New("http(s) repository is not supported"))
					return
				}

				cwd, _ := os.Getwd()
				repo, err := transformRepoUrl(cwd, value)
				if err != nil {
					exitWithError(err)
					return
				}

				_, err = repository.NewRepository(repo)
				if err != nil {
					exitWithError(err)
					return
				}
			}

			config.Set(key, value)
			err := config.Save()
			if err != nil {
				fmt.Println(err)
			}
		}
	},
}

func init() {
}
