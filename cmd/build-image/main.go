package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appscode-images/builder/api"
	"github.com/appscode-images/builder/lib"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	flag "github.com/spf13/pflag"
	shell "gomodules.xyz/go-sh"
	"sigs.k8s.io/yaml"
)

const (
	KeyImageSource   = "org.opencontainers.image.source"
	KeyImageRevision = "org.opencontainers.image.revision"
)

type ImageManifest struct {
	SchemaVersion int                     `json:"schemaVersion"`
	MediaType     string                  `json:"mediaType"`
	Manifests     []PlatformImageManifest `json:"manifests"`
	Config        ImageConfig             `json:"config"`
	Layers        []ImageLayer            `json:"layers"`
	Annotations   map[string]string       `json:"annotations"`
	Labels        map[string]string       `json:"labels"`
}

type PlatformImageManifest struct {
	MediaType string   `json:"mediaType"`
	Size      int      `json:"size"`
	Digest    string   `json:"digest"`
	Platform  Platform `json:"platform"`
}

type Platform struct {
	Architecture string `json:"architecture"`
	Os           string `json:"os"`
}

type ImageConfig struct {
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	Digest    string `json:"digest"`
}

type ImageLayer struct {
	MediaType string `json:"mediaType"`
	Size      int    `json:"size"`
	Digest    string `json:"digest"`
}

func main__() {
	sh := lib.NewShell()
	ts := time.Now().UTC().Format("20060102")

	name := "alpine"
	tag := "3.17.3"
	ref := fmt.Sprintf("%s/%s:%s_%s", api.DAILY_REGISTRY, name, tag, ts)
	ref = "cgr.dev/chainguard/ruby"

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	repoURL, _, err := FindBlock(dir, name, tag)
	if err != nil {
		panic(err)
	}

	ShouldBuild(sh, ref, repoURL)
}

func ShouldBuild(sh *shell.Session, ref string, repoURL string) (bool, error) {
	data, err := crane.Manifest(ref, crane.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		if lib.ImageNotFound(err) {
			return true, nil
		}
		return false, err
	}

	var m ImageManifest
	err = json.Unmarshal(data, &m)
	if err != nil {
		return false, err
	}

	report, err := lib.Scan(sh, ref)
	if err != nil {
		return false, err
	}
	riskOccurrence := lib.SummarizeReport(report)
	// Total: 0 (UNKNOWN: 0, LOW: 0, MEDIUM: 0, HIGH: 0, CRITICAL: 0)
	for _, occurrence := range riskOccurrence {
		if occurrence > 0 {
			return true, nil
		}
	}

	// https://github.com/opencontainers/image-spec/blob/main/annotations.md#pre-defined-annotation-keys
	// org.opencontainers.image.source
	// org.opencontainers.image.revision

	expectedCommitSHA := lib.LastCommitSHA(sh)
	imgSrc := m.Annotations[KeyImageSource]
	imgRev := m.Annotations[KeyImageRevision]
	fmt.Println("ref=", ref,
		"src= expected:", repoURL, " found:", imgSrc,
		"sha= expected:", expectedCommitSHA, " found:", imgRev)
	return imgSrc != repoURL ||
		imgRev != expectedCommitSHA, nil
}

func main_() {
	u2, err := url.Parse("https://github.com/appscode-images/elastic-dockerfiles.git")
	if err != nil {
		panic(err)
	}
	fullname := u2.Path
	fullname = strings.TrimPrefix(fullname, "/")
	fullname = strings.TrimSuffix(fullname, ".git")
	fmt.Println(fullname)
}

func main() {
	var name = flag.String("name", "elastic", "Name of binary")
	var tag = flag.String("tag", "6.8.23", "Tag to be built")
	flag.Parse()

	t := time.Now()
	ts := t.UTC().Format("20060102")
	sh := lib.NewShell()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	libRepoURL, b, err := FindBlock(dir, *name, *tag)
	if err != nil {
		panic(err)
	}
	var repoURL string
	if strings.Contains(libRepoURL, "github.com/"+api.GH_IMG_REPO_OWNER) {
		repoURL = libRepoURL
	} else {
		repoURL = fmt.Sprintf("https://github.com/%s/%s", api.GH_IMG_REPO_OWNER, *name)
	}

	//amd64Ref := fmt.Sprintf("%s/%s:%s_%s_linux_amd64", api.DAILY_REGISTRY, *name, *tag, ts)
	//amd64Yes, err := ShouldBuild(sh, amd64Ref, repoURL, b)
	//if err != nil {
	//	panic(err)
	//}
	//
	//arm64Ref := fmt.Sprintf("%s/%s:%s_%s_linux_arm64", api.DAILY_REGISTRY, *name, *tag, ts)
	//arm64Yes, err := ShouldBuild(sh, arm64Ref, repoURL, b)
	//if err != nil {
	//	panic(err)
	//}

	// if amd64Yes || arm64Yes {
	err = Build(sh, libRepoURL, repoURL, b, *name, *tag, ts)
	if err != nil {
		panic(err)
	}
	// }
}

func Build(sh *shell.Session, libRepoURL, repoURL string, b *api.Block, name, tag, ts string) error {
	ctx := context.Background()
	gh := lib.NewGitHubClient(ctx)

	fullname, err := GetFullName(libRepoURL)
	if err != nil {
		return err
	}
	owner, repo, found := strings.Cut(fullname, "/")
	if !found {
		return fmt.Errorf("invalid repo full name %s", fullname)
	}
	exists, err := lib.GitHubRepoExists(ctx, gh, owner, repo)
	if err != nil {
		return err
	}
	if !exists {
		//err = sh.Command("gh", "repo", "fork", libRepoURL, "--org="+api.GH_IMG_REPO_OWNER, "--fork-name="+name, "--clone=false", "--remote=false").Run()
		//if err != nil {
		//	return err
		//}
		// fork manually
		return fmt.Errorf("fork %s", libRepoURL)
	}

	localRepoDir := filepath.Join("/tmp", name)
	err = os.RemoveAll(localRepoDir)
	if err != nil {
		return err
	}
	err = sh.Command("git", "clone", repoURL, name).Run()
	if err != nil {
		return err
	}
	sh.SetDir(localRepoDir)

	err = sh.Command("git", "remote", "add", "upstream", libRepoURL).Run()
	if err != nil {
		return err
	}

	branch := tag + "-" + ts
	if lib.RemoteBranchExists(sh, branch) {
		err = sh.Command("git", "checkout", "-b", branch, "--track", "origin/"+branch).Run()
		if err != nil {
			return err
		}
	} else if libRepoURL != repoURL {
		// https://stackoverflow.com/a/24084746
		err = sh.Command("git", "fetch", "upstream", b.GitCommit).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "checkout", b.GitCommit).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "checkout", "-b", branch).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "push", "origin", "HEAD", "-f").Run()
		if err != nil {
			return err
		}
	} else if libRepoURL == repoURL {
		// https://stackoverflow.com/a/24084746
		err = sh.Command("git", "checkout", b.GitCommit).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "checkout", "-b", branch).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "push", "origin", "HEAD", "-f").Run()
		if err != nil {
			return err
		}
	}

	amd64Ref := fmt.Sprintf("%s/%s:%s_%s_linux_amd64", api.DAILY_REGISTRY, name, tag, ts)
	amd64Yes, err := ShouldBuild(sh, amd64Ref, repoURL)
	if err != nil {
		panic(err)
	}

	arm64Ref := fmt.Sprintf("%s/%s:%s_%s_linux_arm64", api.DAILY_REGISTRY, name, tag, ts)
	arm64Yes, err := ShouldBuild(sh, arm64Ref, repoURL)
	if err != nil {
		panic(err)
	}

	if !amd64Yes && !arm64Yes {
		return nil
	}

	// https://github.com/kubedb/mysql-init-docker/blob/release-8.0.31/Makefile

	if len(b.Architectures) == 0 {
		b.Architectures = map[string]*api.ArchInfo{
			"amd64": {
				Architecture: "amd64",
			},
			"arm64": {
				Architecture: "arm64",
			},
		}
	}

	var archImages []any
	for arch, info := range b.Architectures {
		if !lib.SupportedArch(arch) {
			continue
		}

		dockerfileDir := filepath.Join("/tmp", name)
		if info.Directory != "" {
			dockerfileDir = filepath.Join("/tmp", name, info.Directory)
		} else if b.Directory != "" {
			dockerfileDir = filepath.Join("/tmp", name, b.Directory)
		}
		sh.SetDir(dockerfileDir)

		img := fmt.Sprintf("%s/%s:%s_%s_%s", api.DAILY_REGISTRY, name, tag, ts, strings.ReplaceAll(lib.Platform(arch), "/", "_"))
		archImages = append(archImages, img)
		args := []any{
			"build", "--platform=" + lib.Platform(arch), "--load", "--pull", "-t", img,
		}
		if info.File != "" {
			args = append(args, "-f", info.File)
		} else if b.File != "" {
			args = append(args, "-f", b.File)
		}
		args = append(args, ".")
		err = sh.Command("docker", args...).Run()
		if err != nil {
			return err
		}
		err = sh.Command("docker", "push", img).Run()
		if err != nil {
			return err
		}

		// > crane mutate ghcr.io/appscode-images/alpine:3.17.3_20231012 -a abc=xyz3 --tag ghcr.io/appscode-images/alpine:3.17.3_20231012
		args = []any{"mutate", img, "--tag=" + img}
		args = append(args, "-a", KeyImageSource+"="+repoURL)
		args = append(args, "-a", KeyImageRevision+"="+lib.LastCommitSHA(sh))
		err = sh.Command("crane", args...).Run()
		if err != nil {
			return err
		}
	}

	// docker manifest create -a $(IMAGE):$(TAG) $(foreach PLATFORM,$(PLATFORM_ARCHS),$(IMAGE):$(TAG)_$(subst /,_,$(PLATFORM)))
	// docker manifest push $(IMAGE):$(TAG)

	img := fmt.Sprintf("%s/%s:%s_%s", api.DAILY_REGISTRY, name, tag, ts)
	args := append([]any{"manifest", "create", "-a", img}, archImages...)
	err = sh.Command("docker", args...).Run()
	if err != nil {
		return err
	}
	err = sh.Command("docker", "manifest", "push", img).Run()
	if err != nil {
		return err
	}

	//// > crane mutate ghcr.io/appscode-images/alpine:3.17.3_20231012 -a abc=xyz3 --tag ghcr.io/appscode-images/alpine:3.17.3_20231012
	//args = []any{"mutate", img, "--tag=" + img}
	//args = append(args, "-a", KeyImageSource+"="+repoURL)
	//args = append(args, "-a", KeyImageRevision+"="+lib.LastCommitSHA(sh))
	//err = sh.Command("crane", args...).Run()
	//if err != nil {
	//	return err
	//}

	return nil
}

func GetFullName(s string) (string, error) {
	u2, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	fullname := u2.Path
	fullname = strings.TrimPrefix(fullname, "/")
	fullname = strings.TrimSuffix(fullname, ".git")
	return fullname, nil
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
