package lib

import (
	"os"
	"path/filepath"
	"strings"
)

const (
	FileBuildTags    = "build_tags.txt"
	FilePromotedTags = "promote_tags.txt"
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

func ListBuildTags(dir, name string) ([]string, error) {
	return listTags(dir, name, FileBuildTags)
}

func ListPromoteTags(dir, name string) ([]string, error) {
	return listTags(dir, name, FilePromotedTags)
}

func listTags(dir, name, tagFile string) ([]string, error) {
	filename := filepath.Join(dir, "library", name, tagFile)
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
