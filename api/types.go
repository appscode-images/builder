package api

import (
	"bytes"
	"gomodules.xyz/sets"
	"strings"
)

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
