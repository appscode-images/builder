package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/appscode-images/builder/api"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-github/v55/github"
	flag "github.com/spf13/pflag"
	"golang.org/x/oauth2"
	shell "gomodules.xyz/go-sh"
	"kubeops.dev/scanner/apis/trivy"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sigs.k8s.io/yaml"
	"strings"
	"time"
)

const (
	skew = 10 * time.Second
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

type T struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Manifests     []struct {
		MediaType string `json:"mediaType"`
		Size      int    `json:"size"`
		Digest    string `json:"digest"`
		Platform  struct {
			Architecture string `json:"architecture"`
			Os           string `json:"os"`
		} `json:"platform"`
	} `json:"manifests"`
	Annotations map[string]string `json:"annotations"`
}

func main() {
	crane_do("alpine", "3.18.4", time.Now())
}

func crane_do(name, tag string, t time.Time) error {
	ts := t.UTC().Format("20060102")

	ref := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, name, tag, ts)
	ref = "cgr.dev/chainguard/ruby"

	data, err := crane.Manifest(ref)
	if err != nil {
		if IsNotFound(err) {
			fmt.Printf("NOT_FOUND %s\n", ref)
			return nil
		}
		return err
	}

	var m ImageManifest
	err = json.Unmarshal(data, &m)
	if err != nil {
		return err
	}

	fmt.Println(m.MediaType)
	return nil
}

func IsNotFound(err error) bool {
	var terr *transport.Error
	if errors.As(err, &terr) {
		return terr.StatusCode == http.StatusNotFound //&& terr.StatusCode != http.StatusForbidden {
	}
	return false
}

// trivy image ubuntu --security-checks vuln --format json --quiet
func scan(sh *shell.Session, img string) (*trivy.SingleReport, error) {
	args := []any{
		"image",
		img,
		"--security-checks", "vuln",
		"--format", "json",
		// "--quiet",
	}
	out, err := sh.Command("trivy", args...).Output()
	if err != nil {
		return nil, err
	}

	var r trivy.SingleReport
	err = trivy.JSON.Unmarshal(out, &r)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func main_() {
	var name *string = flag.String("name", "", "Name of binary")
	var tag *string = flag.String("tag", "", "Tag to be built")

	flag.Parse()

	yes, err := ShouldBuild(*name, *tag)
	if err != nil {
		panic(err)
	}
	if yes {
		dir, err := os.Getwd()
		if err != nil {
			panic(err)
		}

		err = Build(dir, *name, *tag, time.Now())
		if err != nil {
			panic(err)
		}
	}
}

func ShouldBuild(name, tag string) (bool, error) {
	return false, nil
}

func getNewShell() *shell.Session {
	sh := shell.NewSession()
	sh.SetDir("/tmp")
	sh.SetEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN"))

	sh.ShowCMD = true
	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr
	return sh
}

func Build(dir, name, tag string, t time.Time) error {
	ts := t.UTC().Format("20060102")

	sh := getNewShell()

	repoURL, b, err := FindBlock(dir, name, tag)
	if err != nil {
		return err
	}

	ctx := context.Background()
	gh := NewGitHubClient(ctx)
	exists, err := ListOrgRepos(ctx, gh, api.GH_IMG_REPO_OWNER, name)
	if err != nil {
		return err
	}
	if !exists {
		err = sh.Command("gh", "repo", "fork", repoURL, "--org="+api.GH_IMG_REPO_OWNER, "--clone=false", "--remote=false").Run()
		if err != nil {
			return err
		}
	}

	err = sh.Command("git", "clone", fmt.Sprintf("https://github.com/%s/%s", api.GH_IMG_REPO_OWNER, name)).Run()
	if err != nil {
		return err
	}
	sh.SetDir(filepath.Join("/tmp", name))
	err = sh.Command("gh", "remote", "add", "upstream", repoURL).Run()
	if err != nil {
		return err
	}
	err = sh.Command("gh", "fetch", "upstream").Run()
	if err != nil {
		return err
	}

	branch := tag + "-" + ts
	if RemoteBranchExists(sh, branch) {
		err = sh.Command("git", "checkout", branch, "--track", "origin/"+branch).Run()
		if err != nil {
			return err
		}
	} else {
		err = sh.Command("git", "checkout", "-b", branch, "--track", "upstream/"+b.GitCommit).Run()
		if err != nil {
			return err
		}
		err = sh.Command("git", "push", "origin", "HEAD").Run()
		if err != nil {
			return err
		}
	}

	sh.SetDir(filepath.Join("/tmp", name, b.Directory))

	// https://github.com/kubedb/mysql-init-docker/blob/release-8.0.31/Makefile

	var archImages []any
	for _, arch := range api.PLATFORM_ARCHS {
		img := fmt.Sprintf("%s/%s:%s_linunx_%s_%s", api.DOCKER_REGISTRY, name, tag, arch, ts)
		archImages = append(archImages, img)
		err = sh.Command("docker", "build", "--arch=linux/"+arch, "--load", "--pull", "-t", img, ".").Run()
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

func NewGitHubClient(ctx context.Context) *github.Client {
	ts := oauth2.StaticTokenSource(
		&oauth2.Token{AccessToken: os.Getenv("GITHUB_TOKEN")},
	)
	tc := oauth2.NewClient(ctx, ts)
	return github.NewClient(tc)
}

func ListOrgRepos(ctx context.Context, client *github.Client, owner, repo string) (bool, error) {
	for {
		_, _, err := client.Repositories.Get(ctx, owner, repo)
		switch e := err.(type) {
		case *github.RateLimitError:
			time.Sleep(time.Until(e.Rate.Reset.Time.Add(skew)))
			continue
		case *github.AbuseRateLimitError:
			time.Sleep(e.GetRetryAfter())
			continue
		case *github.ErrorResponse:
			if e.Response.StatusCode == http.StatusNotFound {
				log.Println(err)
				break
			} else {
				return false, nil
			}
		default:
			if e != nil {
				return false, err
			}
		}
		return true, nil
	}
}

func RemoteBranchExists(sh *shell.Session, branch string) bool {
	data, err := sh.Command("git", "ls-remote", "--heads", "origin", branch).Output()
	if err != nil {
		panic(err)
	}
	return len(bytes.TrimSpace(data)) > 0
}

func LastCommitSHA(sh *shell.Session) string {
	// git show -s --format=%H
	data, err := sh.Command("git", "show", "-s", "--format=%H").Output()
	if err != nil {
		panic(err)
	}
	commits := strings.Fields(string(data))
	return commits[0]
}
