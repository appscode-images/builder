package lib

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/appscode-images/builder/api"
	"k8s.io/klog/v2"
)

func ParseLibraryFile(filename string) (*api.App, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return ParseLibraryFileContent(filepath.Base(filename), strings.Split(string(data), "\n"))
}

func ParseLibraryFileContent(appName string, lines []string) (*api.App, error) {
	var app api.App

	var curBlock *api.Block
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
				if curBlock.GitCommit == "" {
					curBlock.GitCommit = app.GitCommit
				}
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

		if arch, aprop, found := strings.Cut(curProp, "-"); found {
			if curBlock == nil {
				curBlock = new(api.Block)
			}
			if curBlock.Architectures == nil {
				curBlock.Architectures = map[string]*api.ArchInfo{}
			}
			if _, found := curBlock.Architectures[arch]; !found {
				curBlock.Architectures[arch] = &api.ArchInfo{
					Architecture: arch,
				}
			}

			switch aprop {
			case "Directory":
				curBlock.Architectures[arch].Directory = parts[0]
			case "GitFetch":
				curBlock.Architectures[arch].GitFetch = parts[0]
			case "GitCommit":
				curBlock.Architectures[arch].GitCommit = parts[0]
			case "File":
				curBlock.Architectures[arch].File = parts[0]
			}
		} else {
			switch curProp {
			case "GitRepo":
				app.Name = appName
				app.GitRepo = parts[0]
			case "Tags":
				if curBlock == nil {
					curBlock = new(api.Block)
				}
				curBlock.Tags = append(curBlock.Tags, parts...)
			case "SharedTags":
				if curBlock == nil {
					curBlock = new(api.Block)
				}
				curBlock.Tags = append(curBlock.Tags, parts...)
			case "Architectures":
				if curBlock == nil {
					curBlock = new(api.Block)
				}
				if curBlock.Architectures == nil {
					curBlock.Architectures = map[string]*api.ArchInfo{}
				}
				for _, arch := range parts {
					if _, found := curBlock.Architectures[arch]; !found {
						curBlock.Architectures[arch] = &api.ArchInfo{
							Architecture: arch,
						}
					}
				}
			case "GitCommit":
				if curBlock == nil {
					app.GitCommit = parts[0]
				} else {
					curBlock.GitCommit = parts[0]
				}
			case "Directory":
				if curBlock == nil {
					curBlock = new(api.Block)
				}
				curBlock.Directory = parts[0]
			case "File":
				if curBlock == nil {
					curBlock = new(api.Block)
				}
				curBlock.File = parts[0]
			default:
				klog.V(5).InfoS("ignoring property", before, after)
			}
		}
	}

	// last block
	if curBlock != nil {
		// process cur block
		if curBlock.GitCommit == "" {
			curBlock.GitCommit = app.GitCommit
		}
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
