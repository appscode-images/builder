package main

import (
	"bufio"
	"bytes"
	"fmt"
	"gomodules.xyz/sets"
	"k8s.io/klog/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
)

func main() {
	apps := map[string]AppHistory{}
	dir := "./official-images/library"
	outDir := "./library"

	err := ProcessRepo(apps, dir)
	if err != nil {
		panic(err)
	}
	err = os.MkdirAll(outDir, 0755)
	if err != nil {
		panic(err)
	}

	var buf bytes.Buffer
	for appName, h := range apps {
		buf.Reset()
		buf.WriteString("GitRepo: ")
		buf.WriteString(h.Name)
		buf.WriteRune('\n')

		for _, b := range h.Blocks {
			buf.WriteRune('\n')
			buf.WriteString(b.String())
		}

		filename := filepath.Join(outDir, appName)
		err = os.WriteFile(filename, buf.Bytes(), 0644)
		if err != nil {
			panic(filename + ": " + err.Error())
		}
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
	//	if app, err := Parse(filename); err != nil {
	//		panic(err)
	//	} else {
	//		klog.InfoS("processed", "file", filename, "blocks", len(app.Blocks))
	//	}
	//}

	//// official-images/library/postgres
	//if app, err := Parse("./official-images/library/sl"); err != nil {
	//	panic(err)
	//} else {
	//	fmt.Printf("%+v\n", app)
	//}
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
		app, err := Parse(filename)
		if err != nil {
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

func Parse(filename string) (*App, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var app App

	scanner := bufio.NewScanner(file)
	var line string
	var curBlock *Block
	var curProp string
	// optionally, resize scanner's capacity for lines over 64K, see next example
	for scanner.Scan() {
		line = strings.TrimSpace(scanner.Text())
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
			app.Name = filepath.Base(filename)
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

	return &app, scanner.Err()
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

// Example of how to:
// - Clone a repository into memory
// - Get the HEAD reference
// - Using the HEAD reference, obtain the commit this reference is pointing to
// - Using the commit, obtain its history and print it
func main_git() {
	// Clones the given repository, creating the remote, the local branches
	// and fetching the objects, everything in memory:
	Info("git clone https://github.com/docker-library/postgres")
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: "https://github.com/docker-library/postgres",
	})
	CheckIfError(err)

	// Gets the HEAD history from HEAD, just like this command:
	Info("git log")

	// ... retrieves the branch pointed by HEAD
	ref, err := r.Head()
	CheckIfError(err)

	// ... retrieves the commit history
	since := time.Date(2019, 1, 1, 0, 0, 0, 0, time.UTC)
	until := time.Date(2019, 7, 30, 0, 0, 0, 0, time.UTC)
	cIter, err := r.Log(&git.LogOptions{From: ref.Hash(), Since: &since, Until: &until})
	CheckIfError(err)

	// ... just iterates over the commits, printing it
	err = cIter.ForEach(func(c *object.Commit) error {
		fmt.Println(c)

		c.Files()

		return nil
	})
	CheckIfError(err)
}
