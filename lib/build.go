package lib

import (
	"os"
	"path/filepath"
	"strings"
)

func ListAppTags(dir, name string) ([]string, error) {
	filename := filepath.Join(dir, "library", name, "build_tags.txt")
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	tags := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		tags = append(tags, line)
	}
	return tags, nil
}
