package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/appscode-images/builder/api"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/v1/remote/transport"
	"github.com/google/go-github/v55/github"
	flag "github.com/spf13/pflag"
	"golang.org/x/oauth2"
	shell "gomodules.xyz/go-sh"
	"kubeops.dev/scanner/apis/trivy"
	"sigs.k8s.io/yaml"
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

func main__() {
	sh := getNewShell()
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

func ShouldBuild(sh *shell.Session, ref string, libRepoURL string, b *api.Block) (bool, error) {
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

	report, err := scan(sh, ref)
	if err != nil {
		return false, err
	}
	riskOccurrence := SummarizeReport(report)
	// Total: 0 (UNKNOWN: 0, LOW: 0, MEDIUM: 0, HIGH: 0, CRITICAL: 0)
	for _, occurrence := range riskOccurrence {
		if occurrence > 0 {
			return true, nil
		}
	}

	// https://github.com/opencontainers/image-spec/blob/main/annotations.md#pre-defined-annotation-keys
	// org.opencontainers.image.source
	// org.opencontainers.image.revision

	imgSrc := m.Annotations["org.opencontainers.image.source"]
	imgRev := m.Annotations["org.opencontainers.image.revision"]
	fmt.Println("ref=", ref,
		"src= expected:", libRepoURL, " found:", imgSrc,
		"ref= expected:", b.GitCommit, " found:", imgRev)
	return imgSrc != libRepoURL ||
		imgRev != b.GitCommit, nil
}

func SummarizeReport(report *trivy.SingleReport) map[string]int {
	riskOccurrence := map[string]int{} // risk -> occurrence

	for _, rpt := range report.Results {
		for _, tv := range rpt.Vulnerabilities {
			riskOccurrence[tv.Severity]++
		}
	}

	return riskOccurrence
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

func main() {
	var name = flag.String("name", "alpine", "Name of binary")
	var tag = flag.String("tag", "3.17.3", "Tag to be built")
	flag.Parse()

	t := time.Now()
	ts := t.UTC().Format("20060102")
	ref := fmt.Sprintf("%s/%s:%s_%s", api.DOCKER_REGISTRY, *name, *tag, ts)
	sh := getNewShell()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}
	libRepoURL, b, err := FindBlock(dir, *name, *tag)
	if err != nil {
		panic(err)
	}

	yes, err := ShouldBuild(sh, ref, libRepoURL, b)
	if err != nil {
		panic(err)
	}
	if yes {
		err = Build(sh, libRepoURL, b, *name, *tag, ts)
		if err != nil {
			panic(err)
		}
	}
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

func Build(sh *shell.Session, repoURL string, b *api.Block, name, tag, ts string) error {
	ctx := context.Background()
	gh := NewGitHubClient(ctx)
	exists, err := GitHubRepoExists(ctx, gh, api.GH_IMG_REPO_OWNER, name)
	if err != nil {
		return err
	}
	if !exists {
		err = sh.Command("gh", "repo", "fork", repoURL, "--org="+api.GH_IMG_REPO_OWNER, "--fork-name="+name, "--clone=false", "--remote=false").Run()
		if err != nil {
			return err
		}
	}

	localRepoDir := filepath.Join("/tmp", name)
	err = os.RemoveAll(localRepoDir)
	if err != nil {
		return err
	}
	err = sh.Command("git", "clone", fmt.Sprintf("https://github.com/%s/%s", api.GH_IMG_REPO_OWNER, name)).Run()
	if err != nil {
		return err
	}
	sh.SetDir(localRepoDir)

	err = sh.Command("git", "remote", "add", "upstream", repoURL).Run()
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

		img := fmt.Sprintf("%s/%s:%s_linunx_%s_%s", api.DOCKER_REGISTRY, name, tag, arch, ts)
		archImages = append(archImages, img)
		args := []any{
			"build", "--arch=linux/" + arch, "--load", "--pull", "-t", img,
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

func GitHubRepoExists(ctx context.Context, client *github.Client, owner, repo string) (bool, error) {
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
