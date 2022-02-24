package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
	"github.com/infuseai/artiv/internal/executor"
	"github.com/infuseai/artiv/internal/repository"
)

type ArtifactManager struct {

	// local
	baseDir string // the workspace base dir

	/* the path to store the metadata of the repository.
	If it is in a workspace, the path should be "${baseDir}/.art"*/
	metadataDir string

	// repository
	repo repository.Repository
}

func NewArtifactManager(config ArtConfig) (*ArtifactManager, error) {
	// init the workspace path
	baseDir := config.BaseDir
	if baseDir == "" {
		return nil, errors.New("no workspace specified")
	}

	finfo, err := os.Stat(baseDir)
	if err == nil {
		if !finfo.IsDir() {
			return nil, errors.New(baseDir + " is not a directory")
		}
	}

	// init the metadata path
	metadataDir := config.MetadataDir
	if baseDir == "" {
		return nil, errors.New("no metadata specified")
	}

	// init the repository
	repoStr := config.RepoUrl()
	if repoStr == "" {
		return nil, errors.New("no repository specified")
	}
	repo, err := repository.NewRepository(repoStr)
	if err != nil {
		return nil, err
	}

	return &ArtifactManager{baseDir: baseDir, repo: repo, metadataDir: metadataDir}, nil
}

func (mngr *ArtifactManager) UploadBlob(metadata BlobMetaData) (BlobUploadResult, error) {
	repoPath := MakeObjectPath(metadata.Hash)
	_, err := mngr.repo.Stat(repoPath)
	if err == nil {
		return BlobUploadResult{Skip: true}, nil
	}

	blobPath := filepath.Join(mngr.baseDir, metadata.Path)
	err = mngr.repo.Upload(blobPath, repoPath)
	return BlobUploadResult{Skip: false}, err
}

func (mngr *ArtifactManager) DownloadBlob(metadata BlobMetaData) (BlobDownloadResult, error) {
	hash, err := Sha1SumFromFile(path.Join(mngr.baseDir, metadata.Path))
	if err == nil && hash == metadata.Hash {
		return BlobDownloadResult{Skip: true}, nil
	}

	// fmt.Printf("Skip:     %s\n", metadata.Path)
	// fmt.Printf("download: %s\n", metadata.Path)
	blobPath := filepath.Join(mngr.baseDir, metadata.Path)
	repoPath := MakeObjectPath(metadata.Hash)
	err = mngr.repo.Download(repoPath, blobPath)
	if err != nil {
		return BlobDownloadResult{}, err
	}
	return BlobDownloadResult{Skip: false}, nil
}

func (mngr *ArtifactManager) Commit(commit Commit) error {
	content, hash := MakeCommitMetadata(&commit)
	commitPath := MakeCommitPath(hash)
	localPath := path.Join(mngr.metadataDir, commitPath)
	err := writeGzipFile(content, localPath)
	if err != nil {
		return err
	}

	err = mngr.repo.Upload(localPath, commitPath)
	if err != nil {
		return err
	}

	return err
}

func (mngr *ArtifactManager) AddRef(ref string, commit string) error {
	refPath := MakeRefPath(ref)
	localPath := path.Join(mngr.metadataDir, refPath)
	err := writeFile([]byte(commit), localPath)
	if err != nil {
		return err
	}

	err = mngr.repo.Upload(localPath, refPath)
	if err != nil {
		return err
	}

	return err
}

func (mngr *ArtifactManager) DeleteRef(ref string) error {
	refPath := MakeRefPath(ref)
	localPath := path.Join(mngr.metadataDir, refPath)

	err := deleteFile(localPath)
	if err != nil {
		return err
	}

	err = mngr.repo.Delete(refPath)
	if err != nil {
		return err
	}

	return err
}

func (mngr *ArtifactManager) GetRef(ref string) (string, error) {
	refPath := MakeRefPath(ref)
	localPath := path.Join(mngr.metadataDir, refPath)

	err := mkdirsForFile(localPath)
	if err != nil {
		return "", err
	}

	err = mngr.repo.Download(refPath, localPath)
	if err != nil {
		return "", err
	}

	data, err := readFile(path.Join(mngr.metadataDir, refPath))
	if err != nil {
		return "", err
	}

	hash := string(data)
	if hash == "" {
		return "", fmt.Errorf("the reference %s is empty", ref)
	}

	return hash, nil
}

func (mngr *ArtifactManager) GetCommit(hash string) (*Commit, error) {
	commitPath := MakeCommitPath(hash)
	localPath := path.Join(mngr.metadataDir, commitPath)

	_, err := os.Stat(localPath)
	if err != nil {
		err := mkdirsForFile(localPath)
		if err != nil {
			return nil, err
		}

		err = mngr.repo.Download(commitPath, localPath)
		if err != nil {
			return nil, err
		}
	}

	data, err := readGzipFile(path.Join(mngr.metadataDir, commitPath))
	if err != nil {
		return nil, err
	}

	var commit Commit
	err = json.Unmarshal(data, &commit)
	if err != nil {
		return nil, err
	}

	return &commit, nil
}

func (mngr *ArtifactManager) FindCommitOrReference(refOrCommit string) (string, error) {
	var refPath string
	if refOrCommit == RefLatest {
		refPath = MakeRefPath(RefLatest)
	} else {
		refPath = MakeTagPath(refOrCommit)
	}

	data, err := readFile(path.Join(mngr.metadataDir, refPath))
	if err == nil {
		return string(data), nil
	}

	if len(refOrCommit) >= 4 {
		dirEntries, err := ioutil.ReadDir(path.Join(mngr.metadataDir, "commits"))
		candidates := []string{}

		if err != nil {
			dirEntries = []fs.FileInfo{}
		}

		for _, entry := range dirEntries {
			if entry.IsDir() {
				continue
			}

			if strings.HasPrefix(entry.Name(), refOrCommit) {
				candidates = append(candidates, entry.Name())
			}
		}

		if len(candidates) == 1 {
			return candidates[0], nil
		}
	}

	if refOrCommit == RefLatest {
		return "", ErrEmptyRepository
	} else {
		return "", ReferenceNotFoundError{
			Ref: refOrCommit,
		}
	}
}

// Fetch downloads all the metadata from repository
func (mngr *ArtifactManager) Fetch() error {
	// fetch latest
	mngr.GetRef(RefLatest)

	// fetch tags
	tagEntries, err := mngr.repo.List("refs/tags")
	if err != nil {
		return err
	}

	for _, entry := range tagEntries {
		mngr.GetRef("tags/" + entry.Name())
	}

	// fetch commmits
	commitEntries, err := mngr.repo.List("commits")
	if err != nil {
		return err
	}

	for _, entry := range commitEntries {
		_, err := mngr.GetCommit(entry.Name())
		if err != nil {
			return err
		}
	}

	return nil
}

func (mngr *ArtifactManager) Push(options PushOptions) error {
	parent, err := mngr.GetRef(RefLatest)
	if err != nil {
		parent = ""
	}

	commit, err := mngr.MakeWorkspaceCommit(parent, options.Message)
	if err != nil {
		return err
	}

	result, err := mngr.Diff(DiffOptions{
		LeftRef:     RefLatest,
		RightCommit: commit,
	})
	if err != nil {
		if err != ErrEmptyRepository {
			return err
		} else {
			mngr.Diff(DiffOptions{
				LeftCommit:  mngr.MakeEmptyCommit(),
				RightCommit: commit,
			})
		}
	} else if !result.IsChanged() {
		return nil
	}

	if options.DryRun {
		return nil
	}

	total := len(commit.Blobs)
	uploaded := 0
	skipped := 0

	for _, metadata := range commit.Blobs {
		result, err := mngr.UploadBlob(metadata)
		if result.Skip {
			skipped++
		}
		uploaded++
		if err != nil {
			return fmt.Errorf("cannot upload blob: %s", metadata.Path)
		}
		fmt.Printf("\rupload objects: (%d/%d)", uploaded, total)
		if skipped > 0 {
			fmt.Printf(", skipped: %d", skipped)
		}
	}
	fmt.Printf("\ntotal %d objects uploaded\n", total-skipped)

	_, hash := MakeCommitMetadata(commit)
	fmt.Println("create commit: " + hash)
	mngr.Commit(*commit)
	fmt.Println("update ref: latest -> " + hash)
	mngr.AddRef(RefLatest, hash)
	if options.Tag != nil {
		tag := *options.Tag
		mngr.AddTag(hash, tag)
		fmt.Println("add tag: " + tag + " -> " + hash)
	}

	return nil
}

func (mngr *ArtifactManager) MakeEmptyCommit() *Commit {
	return &Commit{
		CreatedAt: time.Now(),
		Parent:    "",
		Message:   nil,
		Blobs:     []BlobMetaData{},
	}
}

func (mngr *ArtifactManager) MakeWorkspaceCommit(parent string, message *string) (*Commit, error) {
	baseDir := mngr.baseDir
	commit := Commit{
		CreatedAt: time.Now(),
		Parent:    parent,
		Message:   message,
		Blobs:     make([]BlobMetaData, 0),
	}

	tasks := []executor.TaskFunc{}
	mutex := sync.Mutex{}
	err := filepath.Walk(baseDir, func(absPath string, info fs.FileInfo, err error) error {
		if err != nil {
			return ErrWorkspaceNotFound
		}
		task := func(ctx context.Context) error {
			if info.IsDir() {
				return nil
			}

			path := absPath[len(baseDir)+1:]
			if strings.HasPrefix(path, ".art") {
				return nil
			}

			if info.Mode()&os.ModeSymlink != 0 {
				link, _ := os.Readlink(absPath)
				return fmt.Errorf("symbolic link not implemented now. %s -> %s", path, link)
			}

			metadata, err := MakeBlobMetadata(baseDir, path)
			if err != nil {
				return fmt.Errorf("cannot make metadata: %s", path)
			}

			mutex.Lock()
			commit.Blobs = append(commit.Blobs, metadata)
			mutex.Unlock()

			return nil
		}
		tasks = append(tasks, task)
		return nil
	})
	if err != nil {
		return nil, err
	}

	err = executor.ExecuteAll(0, tasks...)
	if err != nil {
		return nil, err
	}

	return &commit, nil
}

func (mngr *ArtifactManager) Pull(options PullOptions) error {
	var err error
	if options.Fetch {
		err = mngr.Fetch()
		if err != nil {
			return err
		}
	}

	refOrCommit := RefLatest
	if options.RefOrCommit != nil {
		refOrCommit = *options.RefOrCommit
	}

	// Make the remote commit
	commitHash, err := mngr.FindCommitOrReference(refOrCommit)
	if err != nil {
		var refPath string
		if refOrCommit == RefLatest {
			refPath = RefLatest
		} else {
			refPath = "tags/" + refOrCommit
		}
		commitHash, err = mngr.GetRef(refPath)
		if err != nil {
			if refOrCommit == RefLatest {
				return ErrEmptyRepository
			} else {
				return ReferenceNotFoundError{Ref: refOrCommit}
			}
		}
	}

	commitRemote, err := mngr.GetCommit(commitHash)
	if err != nil && err != ErrEmptyRepository {
		return err
	}

	// Get the local commit hash
	commitLocal, err := mngr.MakeWorkspaceCommit("", nil)
	if err != nil && err != ErrWorkspaceNotFound {
		return err
	}

	// Diff
	if commitLocal != nil && options.Diff {
		result, err := mngr.Diff(DiffOptions{
			LeftCommit:  commitLocal,
			RightCommit: commitRemote,
		})
		if err != nil {
			return err
		}
		if !result.IsChanged() {
			return nil
		}
	}

	if options.DryRun {
		return nil
	}

	total := len(commitRemote.Blobs)
	downloaded := 0
	skipped := 0
	for _, blob := range commitRemote.Blobs {
		err := mkdirsForFile(path.Join(mngr.baseDir, blob.Path))
		if err != nil {
			return err
		}

		result, err := mngr.DownloadBlob(blob)
		if err != nil {
			return err
		}

		if result.Skip {
			skipped++
		}

		downloaded++
		fmt.Printf("\rdownload objects: (%d/%d)", downloaded, total)
		if skipped > 0 {
			fmt.Printf(", skipped: %d", skipped)
		}
	}
	fmt.Printf("\ntotal %d objects downloaded\n", total-skipped)

	return nil
}

func (mngr *ArtifactManager) ListTags() error {
	err := mngr.Fetch()
	if err != nil {
		return err
	}

	dirEntries, err := ioutil.ReadDir(path.Join(mngr.metadataDir, "refs/tags"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	for _, entry := range dirEntries {
		if entry.IsDir() {
			continue
		}

		fmt.Println(entry.Name())
	}
	return nil
}

func (mngr *ArtifactManager) AddTag(refOrCommit, tag string) error {
	if tag == RefLatest {
		return errors.New("latest cannot be a tag")
	}

	commitHash, err := mngr.FindCommitOrReference(refOrCommit)
	if err != nil {
		return err
	}

	err = mngr.AddRef("tags/"+tag, commitHash)
	if err != nil {
		return err
	}

	return nil
}

func (mngr *ArtifactManager) DeleteTag(tag string) error {
	if tag == RefLatest {
		return errors.New("latest cannot be a tag")
	}

	err := mngr.DeleteRef("tags/" + tag)
	if err != nil {
		return err
	}

	return nil
}

func (mngr *ArtifactManager) List(refOrCommit string) error {
	commitHash, err := mngr.FindCommitOrReference(refOrCommit)
	if err != nil {
		return err
	}

	commit, err := mngr.GetCommit(commitHash)
	if err != nil {
		return err
	}
	for _, blob := range commit.Blobs {
		fmt.Println(blob.Path)
	}

	return nil
}

func (mngr *ArtifactManager) Diff(option DiffOptions) (DiffResult, error) {
	result := DiffResult{}
	type DiffEntry struct {
		left  *BlobMetaData
		right *BlobMetaData
	}
	entries := map[string]DiffEntry{}

	var commitHash string
	var commit *Commit
	var err error
	var added, deleted, changed, renamed int

	// left
	commit = option.LeftCommit
	if commit == nil {
		commitHash, err = mngr.FindCommitOrReference(option.LeftRef)
		if err != nil {
			return DiffResult{}, err
		}

		commit, err = mngr.GetCommit(commitHash)
		if err != nil {
			return result, err
		}
	}
	for i, blob := range commit.Blobs {
		entry := entries[blob.Path]
		entry.left = &commit.Blobs[i]
		entries[blob.Path] = entry
	}

	// right
	commit = option.RightCommit
	if commit == nil {
		commitHash, err = mngr.FindCommitOrReference(option.RightRef)
		if err != nil {
			return result, err
		}

		commit, err = mngr.GetCommit(commitHash)
		if err != nil {
			return result, err
		}
	}

	for i, blob := range commit.Blobs {
		entry := entries[blob.Path]
		entry.right = &commit.Blobs[i]
		entries[blob.Path] = entry
	}

	// diff
	paths := []string{}
	for path, _ := range entries {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	// merge the "added" and "deleted" with the same content to "renamed"
	mapAdded := map[string][]string{}
	mapDeleted := map[string][]string{}
	mapRenamed := map[string][]string{}
	mapChanged := map[string][]string{}

	myappend := func(s []string, item string) []string {
		if s != nil {
			return append(s, item)
		} else {
			return []string{item}
		}

	}

	for _, path := range paths {
		entry := entries[path]
		if entry.left == nil && entry.right != nil {
			mapAdded[entry.right.Hash] = myappend(mapAdded[entry.right.Hash], path)
		} else if entry.left != nil && entry.right == nil {
			mapDeleted[entry.left.Hash] = myappend(mapDeleted[entry.left.Hash], path)
		} else if entry.left.Hash != entry.right.Hash {
			mapChanged[entry.left.Hash] = myappend(mapChanged[entry.left.Hash], path)
		}
	}

	for hash, addedPaths := range mapAdded {
		deleledPaths := mapDeleted[hash]
		if deleledPaths == nil {
			continue
		}

		// n = min(added, deleted)
		n := len(addedPaths)
		if len(deleledPaths) < n {
			n = len(deleledPaths)
		}

		//
		renamedPaths := []string{}
		for i := 0; i < n; i++ {
			renamedPaths = append(renamedPaths, fmt.Sprintf("%s -> %s", deleledPaths[i], addedPaths[i]))
		}
		mapAdded[hash] = addedPaths[n:]
		mapDeleted[hash] = deleledPaths[n:]
		mapRenamed[hash] = renamedPaths
	}

	// print the changed paths
	for _, paths := range mapAdded {
		for _, path := range paths {
			color.HiGreen(fmt.Sprintf("+ %s\n", path))
			added++
		}
	}

	for _, paths := range mapDeleted {
		for _, path := range paths {
			color.HiRed(fmt.Sprintf("- %s\n", path))
			deleted++
		}
	}

	for _, paths := range mapChanged {
		for _, path := range paths {
			color.HiYellow(fmt.Sprintf("C %s\n", path))
			changed++
		}
	}

	for _, paths := range mapRenamed {
		for _, path := range paths {
			color.HiYellow(fmt.Sprintf("R %s\n", path))
			renamed++
		}
	}

	// show the summary
	if added == 0 && deleted == 0 && changed == 0 && renamed == 0 {
		fmt.Println("no changed")
	} else {
		fmt.Printf("%d changed, %d added(+), %d deleted(-)\n", changed+renamed, added, deleted)

	}

	return DiffResult{
		Added:   added,
		Deleted: deleted,
		Renamed: renamed,
		Changed: changed,
	}, nil
}

func (mngr *ArtifactManager) Log(refOrCommit string) error {
	err := mngr.Fetch()
	if err != nil {
		return err
	}

	type RefEntry struct {
		refType string
		ref     string
	}

	commitIndex := map[string][]RefEntry{}

	// get latest
	latestPath := path.Join(mngr.metadataDir, "refs/latest")
	data, err := readFile(latestPath)
	if err == nil {
		commitHash := string(data)
		commitIndex[commitHash] = []RefEntry{{
			refType: RefLatest,
			ref:     RefLatest,
		}}
	} else {
		return ErrEmptyRepository
	}

	// get reference
	refTagsDir := path.Join(mngr.metadataDir, "refs/tags")
	dirEntries, err := ioutil.ReadDir(refTagsDir)
	if err == nil {
		for _, entry := range dirEntries {
			ref := entry.Name()
			if entry.IsDir() {
				continue
			}

			data, err := readFile(path.Join(refTagsDir, entry.Name()))
			if err != nil {
				return err
			}

			commitHash := string(data)
			refEntry := RefEntry{
				refType: "tag",
				ref:     ref,
			}
			if commitIndex[commitHash] != nil {
				commitIndex[commitHash] = append(commitIndex[commitHash], refEntry)
			} else {
				commitIndex[commitHash] = []RefEntry{refEntry}
			}
		}
	}

	// log from refOrCommit
	commitHash, err := mngr.FindCommitOrReference(refOrCommit)
	if err != nil {
		return err
	}
	for count := 0; commitHash != "" && count < 1000; count++ {
		commit, err := mngr.GetCommit(commitHash)
		if err != nil {
			return err
		}

		message := ""
		if commit.Message != nil {
			message = *commit.Message
		}

		createdAt := commit.CreatedAt.Format("2006-01-02 15:04 -0700")

		color.Set(color.FgYellow)
		fmt.Printf("%s ", commitHash[:8])
		color.Set(color.FgHiBlack)
		fmt.Printf("%s ", createdAt)

		if commitIndex[commitHash] != nil {
			first := true
			color.Set(color.FgYellow)
			fmt.Print("(")
			for _, refEntry := range commitIndex[commitHash] {
				if !first {
					color.Set(color.FgYellow)
					fmt.Print(", ")
				}

				if refEntry.refType == RefLatest {
					color.Set(color.FgHiGreen)
				} else {
					color.Set(color.FgHiRed)
				}

				fmt.Print(refEntry.ref)
				first = false
			}
			color.Set(color.FgYellow)
			fmt.Print(") ")
		}

		color.Set(color.FgHiWhite)
		fmt.Printf("%s\n", message)
		color.Unset()

		commitHash = commit.Parent
		count++
	}

	return nil
}
