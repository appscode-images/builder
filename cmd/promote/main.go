package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appscode-images/builder/api"
	"github.com/appscode-images/builder/lib"
	"github.com/pkg/errors"
)

func main() {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	if err := PromoteTags(dir); err != nil {
		panic(err)
	}
}

func PromoteTags(dir string) error {
	sh := lib.NewShell()

	libDir := filepath.Join(dir, "library")
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		tags, err := lib.ListPromoteTags(dir, entry.Name())
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if len(tags) == 0 {
			continue
		}

		for _, tsTag := range tags {
			idx := strings.LastIndex(tsTag, "_")
			if idx == -1 {
				continue
			}
			tag := tsTag[:idx]

			srcRef := fmt.Sprintf("%s/%s:%s", api.DAILY_REGISTRY, entry.Name(), tsTag)
			dstRef := fmt.Sprintf("%s/%s:%s", api.DOCKER_REGISTRY, entry.Name(), tag)

			err := sh.Command("crane", "cp", srcRef, dstRef).Run()
			if err != nil {
				return errors.Wrapf(err, "failed to cp %s to %s", srcRef, dstRef)
			}
		}

		_ = os.Remove(filepath.Join(libDir, entry.Name(), lib.FilePromotedTags))
	}
	return nil
}
