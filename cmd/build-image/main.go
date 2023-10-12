package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/appscode-images/builder/lib"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/appscode-images/builder/api"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	flag "github.com/spf13/pflag"
	shell "gomodules.xyz/go-sh"
	"sigs.k8s.io/yaml"
)

const (
	skew             = 10 * time.Second
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
	ref := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, name, tag, ts)
	ref = "cgr.dev/chainguard/ruby"

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	repoURL, b, err := FindBlock(dir, name, tag)
	if err != nil {
		panic(err)
	}

	ShouldBuild(sh, ref, repoURL, b)
}

func ShouldBuild(sh *shell.Session, ref string, repoURL string, b *api.Block) (bool, error) {
	data, err := crane.Manifest(ref, crane.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		if IsNotFound(err) {
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

	imgSrc := m.Annotations[KeyImageSource]
	imgRev := m.Annotations[KeyImageRevision]
	fmt.Println("ref=", ref,
		"src= expected:", repoURL, " found:", imgSrc,
		"ref= expected:", b.GitCommit, " found:", imgRev)
	return imgSrc != repoURL ||
		imgRev != b.GitCommit, nil
}

func IsNotFound(err error) bool {
	var terr *transport.Error
	if errors.As(err, &terr) {
		return terr.StatusCode == http.StatusNotFound //&& terr.StatusCode != http.StatusForbidden {
	}
	return false
}

func main() {
	var name = flag.String("name", "alpine", "Name of binary")
	var tag = flag.String("tag", "3.18.4", "Tag to be built")
	flag.Parse()

	t := time.Now()
	ts := t.UTC().Format("20060102")
	ref := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, *name, *tag, ts)
	sh := lib.NewShell()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	libRepoURL, b, err := FindBlock(dir, *name, *tag)
	if err != nil {
		panic(err)
	}
	repoURL := fmt.Sprintf("https://github.com/%s/%s", api.GH_IMG_REPO_OWNER, *name)

	yes, err := ShouldBuild(sh, ref, repoURL, b)
	if err != nil {
		panic(err)
	}
	if yes {
		err = Build(sh, libRepoURL, repoURL, b, *name, *tag, ts)
		if err != nil {
			panic(err)
		}
	}
}

func Build(sh *shell.Session, libRepoURL, repoURL string, b *api.Block, name, tag, ts string) error {
	ctx := context.Background()
	gh := lib.NewGitHubClient(ctx)
	exists, err := lib.GitHubRepoExists(ctx, gh, api.GH_IMG_REPO_OWNER, name)
	if err != nil {
		return err
	}
	if !exists {
		err = sh.Command("gh", "repo", "fork", libRepoURL, "--org="+api.GH_IMG_REPO_OWNER, "--fork-name="+name, "--clone=false", "--remote=false").Run()
		if err != nil {
			return err
		}
	}

	localRepoDir := filepath.Join("/tmp", name)
	err = os.RemoveAll(localRepoDir)
	if err != nil {
		return err
	}
	err = sh.Command("git", "clone", repoURL).Run()
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
	} else {
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
		err = sh.Command("git", "push", "origin", "HEAD").Run()
		if err != nil {
			return err
		}
	}

	// https://github.com/kubedb/mysql-init-docker/blob/release-8.0.31/Makefile

	var archImages []any
	for arch, info := range b.Architectures {
		if !contains(api.PLATFORM_ARCHS, arch) {
			continue
		}

		dockerfileDir := filepath.Join("/tmp", name)
		if info.Directory != "" {
			dockerfileDir = filepath.Join("/tmp", name, info.Directory)
		} else if b.Directory != "" {
			dockerfileDir = filepath.Join("/tmp", name, b.Directory)
		}
		sh.SetDir(dockerfileDir)

		img := fmt.Sprintf("%s/%s:%s_%s_linux_%s", api.DOCKER_REGISTRY, name, tag, ts, arch)
		archImages = append(archImages, img)
		args := []any{
			"build", "--platform=linux/" + arch, "--load", "--pull", "-t", img,
		}
		if info.File != "" {
			args = append(args, "-f", info.File)
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
	}

	// docker manifest create -a $(IMAGE):$(TAG) $(foreach PLATFORM,$(PLATFORM_ARCHS),$(IMAGE):$(TAG)_$(subst /,_,$(PLATFORM)))
	// docker manifest push $(IMAGE):$(TAG)

	img := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, name, tag, ts)
	args := append([]any{"manifest", "create", "-a", img}, archImages...)
	err = sh.Command("docker", args...).Run()
	if err != nil {
		return err
	}
	err = sh.Command("docker", "manifest", "push", img).Run()
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
