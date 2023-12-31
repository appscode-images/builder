package api

import (
	"bytes"
	"fmt"
	"gomodules.xyz/sets"
	"sort"
	"strings"
)

const (
	GH_IMG_REPO_OWNER = "appscode-images"
	DOCKER_REGISTRY   = "ghcr.io/" + GH_IMG_REPO_OWNER
	DAILY_REGISTRY    = "ghcr.io/" + GH_IMG_REPO_OWNER + "/daily"
)

type AppHistory struct {
	Name      string
	GitRepo   string
	KnownTags sets.String
	Blocks    []Block
}

type App struct {
	Name      string
	GitRepo   string
	GitCommit string
	Blocks    []Block
}

type Block struct {
	Tags          []string
	Architectures map[string]*ArchInfo
	GitCommit     string
	Directory     string
	File          string
}

type ArchInfo struct {
	Architecture string
	Directory    string
	GitFetch     string
	GitCommit    string
	File         string
}

func (b Block) String() string {
	var buf bytes.Buffer
	if len(b.Tags) > 0 {
		buf.WriteString("Tags: ")
		buf.WriteString(strings.Join(b.Tags, ","))
		buf.WriteRune('\n')
	}
	if len(b.Architectures) > 0 {
		archs := make([]string, 0, len(b.Architectures))
		for arch := range b.Architectures {
			archs = append(archs, arch)
		}
		sort.Strings(archs)
		for _, arch := range archs {
			info := b.Architectures[arch]
			if info.Directory != "" {
				buf.WriteString(fmt.Sprintf("%s-Directory: %s\n", arch, info.Directory))
			}
			if info.GitCommit != "" {
				buf.WriteString(fmt.Sprintf("%s-GitCommit: %s\n", arch, info.GitCommit))
			}
			if info.GitFetch != "" {
				buf.WriteString(fmt.Sprintf("%s-GitFetch: %s\n", arch, info.GitFetch))
			}
			if info.File != "" {
				buf.WriteString(fmt.Sprintf("%s-File: %s\n", arch, info.File))
			}
		}

		buf.WriteString("Architectures: ")
		buf.WriteString(strings.Join(archs, ","))
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
	if len(b.File) > 0 {
		buf.WriteString("File: ")
		buf.WriteString(b.File)
		buf.WriteRune('\n')
	}
	return buf.String()
}
