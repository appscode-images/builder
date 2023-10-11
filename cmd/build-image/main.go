package main

import (
	"github.com/appscode-images/builder/api"
	flag "github.com/spf13/pflag"
	shell "gomodules.xyz/go-sh"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
	"time"
)

func main() {
	/*
		var name *string = flag.String("name", "", "Name of binary")
		var tag *string = flag.String("tag", "", "Tag to be built")
		var ts *string = flag.String("timestamo", time.Now().UTC().Format(time.RFC3339), "Timestamp")
	*/
	flag.Parse()
}

func Build(dir, name, tag string, t time.Time) error {
	repoURL, b, err := FindBlock(dir, name, tag)
	if err != nil {
		return err
	}

	sh := shell.NewSession()
	sh.ShowCMD = true
	sh.SetDir("/tmp")

	sh.Command("echo", "hello").Run()

	return nil
}

func FindBlock(dir, name, tag string) (string, *api.Block, error) {
	filename := filepath.Join(dir, "library", name, "app.json")
	data, err := os.ReadFile(filename)
	if err != nil {
		return "", nil, err
	}

	var h api.AppHistory
	err = yaml.Unmarshal(data, &h)
	if err != nil {
		return "", nil, err
	}

	for _, b := range h.Blocks {
		if contains(b.Tags, tag) {
			return h.GitRepo, &b, nil
		}
	}
	return h.GitRepo, nil, nil
}

func contains(arr []string, s string) bool {
	for _, x := range arr {
		if x == s {
			return true
		}
	}
	return false
}
