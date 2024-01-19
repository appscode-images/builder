package lib

import (
	"errors"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
)

const (
	FileCherrypicks = "cherrypicks.yaml"
)

func LoadCherrypicks(dir, name string) (map[string][]string, error) {
	filename := filepath.Join(dir, "library", name, FileCherrypicks)
	data, err := os.ReadFile(filename)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}

	var result map[string][]string
	err = yaml.Unmarshal(data, &result)
	if err != nil {
		return nil, err
	}

	return result, nil
}
