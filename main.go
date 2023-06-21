package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pkg/errors"
	"gomodules.xyz/semvers"
	"gomodules.xyz/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

// Read from Git directly
func main_git() {
	apps := map[string]AppHistory{}
	outDir := "./library"

	err := ProcessGitRepo(apps, true)
	CheckIfError(err)

	err = PrintUnifiedHistory(outDir, apps)
	if err != nil {
		panic(err)
	}
}

func ProcessGitRepo(apps map[string]AppHistory, fullHistory bool) error {
	repoURL := "https://github.com/docker-library/official-images"

	// Clones the given repository, creating the remote, the local branches
	// and fetching the objects, everything in memory:
	Info("git clone " + repoURL)
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: repoURL,
	})
	if err != nil {
		return err
	}

	// Gets the HEAD history from HEAD, just like this command:
	Info("git log")

	// ... retrieves the branch pointed by HEAD
	ref, err := r.Head()
	if err != nil {
		return err
	}

	// ... retrieves the commit history
	opts := git.LogOptions{From: ref.Hash()}
	if !fullHistory {
		from := time.Now().UTC()
		to := from.Add(-14 * 24 * time.Hour)
		opts.Since = &to
		opts.Until = &from
	}
	cIter, err := r.Log(&opts)
	if err != nil {
		return err
	}

	return cIter.ForEach(ProcessCommit(apps))
}

func ProcessCommit(apps map[string]AppHistory) func(c *object.Commit) error {
	return func(c *object.Commit) error {
		files, err := c.Files()
		if err != nil {
			return err
		}
		return files.ForEach(func(file *object.File) error {
			if !strings.HasPrefix(file.Name, "library/") {
				return nil
			}

			lines, err := file.Lines()
			if err != nil {
				return err
			}
			app, err := ParseLibraryFileContent(filepath.Base(file.Name), lines)
			if err != nil || app == nil {
				return err
			}

			klog.InfoS("processed", "commit", c.ID(), "file", file.Name, "blocks", len(app.Blocks))

			h, found := apps[app.Name]
			if !found {
				h = AppHistory{
					Name:      app.Name,
					GitRepo:   app.GitRepo,
					KnownTags: sets.NewString(),
					Blocks:    nil,
				}
			}
			GatherHistory(&h, app)
			apps[app.Name] = h

			return nil
		})
	}
}

func main() {
	apps := map[string]AppHistory{}
	dir := "./official-images/library"
	outDir := "./library"

	err := ProcessRepo(apps, dir)
	if err != nil {
		panic(err)
	}
	err = PrintUnifiedHistory(outDir, apps)
	if err != nil {
		panic(err)
	}

	//entries, err := os.ReadDir(dir)
	//if err != nil {
	//	panic(err)
	//}
	//for _, entry := range entries {
	//	if entry.IsDir() {
	//		continue
	//	}
	//
	//	filename := filepath.Join(dir, entry.Name())
	//	if app, err := ParseLibraryFile(filename); err != nil {
	//		panic(err)
	//	} else {
	//		klog.InfoS("processed", "file", filename, "blocks", len(app.Blocks))
	//	}
	//}

	//// official-images/library/postgres
	//if app, err := ParseLibraryFile("./official-images/library/sl"); err != nil {
	//	panic(err)
	//} else {
	//	fmt.Printf("%+v\n", app)
	//}
}

func PrintUnifiedHistory(outDir string, apps map[string]AppHistory) error {
	err := os.MkdirAll(outDir, 0755)
	if err != nil {
		return err
	}

	var buf bytes.Buffer
	for appName, h := range apps {
		dir := filepath.Join(outDir, appName)
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}

		buf.Reset()
		buf.WriteString("GitRepo: ")
		buf.WriteString(h.GitRepo)
		buf.WriteRune('\n')

		for _, b := range h.Blocks {
			buf.WriteRune('\n')
			buf.WriteString(b.String())
		}

		filename := filepath.Join(dir, "app.txt")
		err = os.WriteFile(filename, buf.Bytes(), 0644)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}

		filename = filepath.Join(dir, "app.json")
		data, err := json.MarshalIndent(h, "", "  ")
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}

		filename = filepath.Join(dir, "app.yaml")
		data, err = yaml.Marshal(h)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}

		filename = filepath.Join(dir, "tags.txt")
		data = []byte(strings.Join(h.KnownTags.List(), "\n"))
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}

		{
			tags := make([]string, 0, h.KnownTags.Len())
			for v := range h.KnownTags {
				if _, err := semver.NewVersion(v); err == nil {
					tags = append(tags, v)
				}
			}
			semvers.SortVersions(tags, func(vi, vj string) bool {
				return !semvers.Compare(vi, vj)
			})
			filename = filepath.Join(dir, "semver.txt")
			data = []byte(strings.Join(tags, "\n"))
			err = os.WriteFile(filename, data, 0644)
			if err != nil {
				return errors.Wrap(err, "file: "+filename)
			}
		}
	}
	return nil
}

func ProcessRepo(apps map[string]AppHistory, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := filepath.Join(dir, entry.Name())
		app, err := ParseLibraryFile(filename)
		if err != nil || app == nil {
			return err
		}
		klog.InfoS("processed", "file", filename, "blocks", len(app.Blocks))

		h, found := apps[app.Name]
		if !found {
			h = AppHistory{
				Name:      app.Name,
				GitRepo:   app.GitRepo,
				KnownTags: sets.NewString(),
				Blocks:    nil,
			}
		}
		GatherHistory(&h, app)
		apps[app.Name] = h
	}

	return nil
}

func GatherHistory(h *AppHistory, app *App) {
	for _, b := range app.Blocks {
		if nb := processBlock(h, &b); nb != nil {
			h.Blocks = append(h.Blocks, *nb)
		}
	}
}

func processBlock(h *AppHistory, b *Block) *Block {
	var result *Block

	newTags := make([]string, 0, len(b.Tags))
	for _, tag := range b.Tags {
		if !h.KnownTags.Has(tag) {
			newTags = append(newTags, tag)
		}
	}
	if len(newTags) > 0 {
		result = &Block{
			Tags:          newTags,
			Architectures: b.Architectures,
			GitCommit:     b.GitCommit,
			Directory:     b.Directory,
		}
		h.KnownTags.Insert(newTags...)
	}
	return result
}

type AppHistory struct {
	Name      string
	GitRepo   string
	KnownTags sets.String
	Blocks    []Block
}

type App struct {
	Name    string
	GitRepo string
	Blocks  []Block
}

type Block struct {
	Tags          []string
	Architectures []string
	GitCommit     string
	Directory     string
}

func (b Block) String() string {
	var buf bytes.Buffer
	if len(b.Tags) > 0 {
		buf.WriteString("Tags: ")
		buf.WriteString(strings.Join(b.Tags, ","))
		buf.WriteRune('\n')
	}
	if len(b.Architectures) > 0 {
		buf.WriteString("Architectures: ")
		buf.WriteString(strings.Join(b.Architectures, ","))
		buf.WriteRune('\n')
	}
	if len(b.GitCommit) > 0 {
		buf.WriteString("GitCommit: ")
		buf.WriteString(b.GitCommit)
		buf.WriteRune('\n')
	}
	if len(b.Directory) > 0 {
		buf.WriteString("Directory: ")
		buf.WriteString(b.Directory)
		buf.WriteRune('\n')
	}
	return buf.String()
}

func ParseLibraryFile(filename string) (*App, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseLibraryFileContent(filepath.Base(filename), strings.Split(string(data), "\n"))
}

func ParseLibraryFileContent(appName string, lines []string) (*App, error) {
	var app App

	var curBlock *Block
	var curProp string
	// optionally, resize scanner's capacity for lines over 64K, see next example
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			continue
		}

		if line == "" {
			if curBlock != nil {
				// process cur block
				app.Blocks = append(app.Blocks, *curBlock)
			}
			curBlock = nil
			curProp = ""
			continue
		}

		before, after, found := strings.Cut(line, ":")
		var parts []string
		if found {
			curProp = before
			parts = strings.Split(after, ",")
		} else {
			parts = strings.Split(before, ",")
		}
		parts = filter(parts)

		switch curProp {
		case "GitRepo":
			app.Name = appName
			app.GitRepo = parts[0]
		case "Tags":
			if curBlock == nil {
				curBlock = new(Block)
			}
			curBlock.Tags = append(curBlock.Tags, parts...)
		case "Architectures":
			if curBlock == nil {
				curBlock = new(Block)
			}
			curBlock.Architectures = append(curBlock.Architectures, parts...)
		case "GitCommit":
			if curBlock == nil {
				curBlock = new(Block)
			}
			curBlock.GitCommit = parts[0]
		case "Directory":
			if curBlock == nil {
				curBlock = new(Block)
			}
			curBlock.Directory = parts[0]
		default:
			klog.V(5).InfoS("ignoring property", before, after)
		}
	}

	// last block
	if curBlock != nil {
		// process cur block
		app.Blocks = append(app.Blocks, *curBlock)
	}

	// eg: ./official-images/library/sourcemage
	if app.Name == "" {
		return nil, nil
	}
	return &app, nil
}

func filter(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
