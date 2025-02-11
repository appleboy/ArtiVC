package cmd

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/infuseai/artivc/internal/core"
	"github.com/infuseai/artivc/internal/repository"
	"github.com/spf13/cobra"
)

var cloneCommand = &cobra.Command{
	Use:                   "clone <repository> [<dir>]",
	Short:                 "Clone a workspace",
	DisableFlagsInUseLine: true,
	Example: `  # clone a workspace with local repository
  avc clone /path/to/mydataset

  # clone a workspace with s3 repository
  avc clone s3://mybucket/path/to/mydataset`,
	Args: cobra.RangeArgs(1, 2),
	Run: func(cmd *cobra.Command, args []string) {
		cwd, _ := os.Getwd()
		repo, err := transformRepoUrl(cwd, args[0])
		if err != nil {
			exitWithError(err)
			return
		}

		if strings.HasPrefix(repo, "http") && !repository.IsAzureStorageUrl(repo) {
			exitWithError(errors.New("clone not support under http(s) repo"))
			return
		}

		_, err = repository.NewRepository(repo)
		if err != nil {
			exitWithError(err)
			return
		}

		destDir, err := parseRepoName(repo)
		if err != nil {
			exitWithError(err)
			return
		}

		if len(args) > 1 {
			destDir = args[1]
		}

		baseDir := filepath.Join(cwd, destDir)
		err = os.Mkdir(baseDir, fs.ModePerm)
		if err == nil || (os.IsExist(err) && isDirEmpty(baseDir)) {
			// pass
		} else if os.IsExist(err) {
			exitWithError(fmt.Errorf("fatal: destination path '%s' already exists and is not an empty directory.", destDir))
			return
		} else {
			exitWithError(fmt.Errorf("fatal: cannot create destination path '%s'.", destDir))
			return
		}
		fmt.Printf("Cloning into '%s'...\n", destDir)

		core.InitWorkspace(baseDir, repo)

		config, err := core.LoadConfig(baseDir)
		if err != nil {
			exitWithError(err)
			return
		}

		mngr, err := core.NewArtifactManager(config)
		if err != nil {
			exitWithError(err)
			return
		}

		err = mngr.Pull(core.PullOptions{})
		if err != nil {
			os.RemoveAll(baseDir) //  remove created dir
			exitWithError(err)
			return
		}
	},
}

func init() {
}
