package lib

import (
	"os"
	"path/filepath"
	"strings"
)

func SupportedArch(arch string) bool {
	return arch == "amd64" ||
		arch == "x86_64" ||
		arch == "arm64" ||
		arch == "arm64v8" ||
		arch == "aarch64"
}

func Platform(arch string) string {
	switch arch {
	case "amd64", "x86_64":
		return "linux/amd64"
	case "arm64", "arm64v8", "aarch64":
		return "linux/arm64"
	default:
		panic("unknown arch: " + arch)
	}
}

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
