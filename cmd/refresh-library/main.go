package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
	"github.com/appscode-images/builder/api"
	"github.com/appscode-images/builder/lib"
	"github.com/go-git/go-git/v5"
	. "github.com/go-git/go-git/v5/_examples"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pkg/errors"
	flag "github.com/spf13/pflag"
	"gomodules.xyz/semvers"
	"gomodules.xyz/sets"
	"k8s.io/klog/v2"
	"sigs.k8s.io/yaml"
)

var skipApps sets.String

// Read from Git directly
func main() {
	skipList := []string{
		"druid",
		"ferretdb",
		"kibana",
		"logstash",
		"opensearch",
		"opensearch-dashboards",
		"valkey",
	}
	flag.StringSliceVar(&skipList, "skip", skipList, "Skip official image (because manually maintained)")
	flag.Parse()

	skipApps = sets.NewString(skipList...)

	apps := map[string]api.AppHistory{}
	outDir := "./library"

	err := ProcessGitRepo(apps, true)
	CheckIfError(err)

	err = PrintUnifiedHistory(outDir, apps)
	if err != nil {
		panic(err)
	}
}

func ProcessGitRepo(apps map[string]api.AppHistory, fullHistory bool) error {
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

func ProcessCommit(apps map[string]api.AppHistory) func(c *object.Commit) error {
	return func(c *object.Commit) error {
		files, err := c.Files()
		if err != nil {
			return err
		}
		return files.ForEach(func(file *object.File) error {
			if !strings.HasPrefix(file.Name, "library/") {
				return nil
			}

			appName := filepath.Base(file.Name)
			if skipApps.Has(appName) {
				klog.InfoS("skipping", "app", appName)
				return nil
			}

			lines, err := file.Lines()
			if err != nil {
				return err
			}
			app, err := lib.ParseLibraryFileContent(appName, lines)
			if err != nil || app == nil {
				return err
			}

			klog.InfoS("processed", "commit", c.ID(), "file", file.Name, "blocks", len(app.Blocks))

			h, found := apps[app.Name]
			if !found {
				h = api.AppHistory{
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

func main_local() {
	apps := map[string]api.AppHistory{}
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

func PrintUnifiedHistory(outDir string, apps map[string]api.AppHistory) error {
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
		tags := h.KnownTags.List()
		semvers.SortVersions(tags, func(vi, vj string) bool {
			return !semvers.Compare(vi, vj)
		})
		data = []byte(strings.Join(tags, "\n"))
		err = os.WriteFile(filename, data, 0644)
		if err != nil {
			return errors.Wrap(err, "file: "+filename)
		}

		{
			tags := make([]string, 0, h.KnownTags.Len())
			for tag := range h.KnownTags {
				if _, err := semver.NewVersion(tag); err == nil {
					tags = append(tags, tag)
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

var acceptedPreReleases = sets.NewString(
	"",
	"bullseye",
	"bookworm",
	"alpine",
	"centos",
	"management-alpine", // rabbitmq
	"management",        // rabbitmq
	"slim",              // debian
	"jammy",             // ubuntu
	"focal",             // ubuntu
	"temurin",           // java
	"openjdk",           // java
)

func SupportedPreRelease(v *semver.Version) bool {
	_, found := acceptedPreReleases[v.Prerelease()]
	return found
}

func ProcessRepo(apps map[string]api.AppHistory, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := filepath.Join(dir, entry.Name())
		app, err := lib.ParseLibraryFile(filename)
		if err != nil || app == nil {
			return err
		}
		klog.InfoS("processed", "file", filename, "blocks", len(app.Blocks))

		h, found := apps[app.Name]
		if !found {
			h = api.AppHistory{
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

func GatherHistory(h *api.AppHistory, app *api.App) {
	for _, b := range app.Blocks {
		if nb := processBlock(h, &b); nb != nil {
			h.Blocks = append(h.Blocks, *nb)
		}
	}
}

func processBlock(h *api.AppHistory, b *api.Block) *api.Block {
	var result *api.Block

	newTags := make([]string, 0, len(b.Tags))
	for _, tag := range b.Tags {
		if !h.KnownTags.Has(tag) {
			newTags = append(newTags, tag)
		}
	}
	if len(newTags) > 0 {
		result = &api.Block{
			Tags:          newTags,
			Architectures: b.Architectures,
			GitCommit:     b.GitCommit,
			Directory:     b.Directory,
			File:          b.File,
		}
		h.KnownTags.Insert(newTags...)
	}
	return result
}
