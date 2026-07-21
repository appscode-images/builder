package main

// The images published from https://github.com/mysql/mysql-docker (currently only
// mysql-router) are not Docker Official Images, so docker-library/official-images
// carries no manifest for them and ProcessGitRepo can never discover them.
//
// Version data is read from the upstream repo itself instead: mysql-<product>/VERSION
// names the series that carries the latest and latest LTS releases, and each
// mysql-<product>/<series>/Dockerfile pins the exact package version it installs.

import (
	"path"
	"regexp"
	"sort"

	"github.com/Masterminds/semver/v3"
	"github.com/appscode-images/builder/api"
	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/storage/memory"
	"github.com/pkg/errors"
	"gomodules.xyz/sets"
	"k8s.io/klog/v2"
)

const MySQLDockerRepo = "https://github.com/mysql/mysql-docker.git"

// MySQLDockerProduct is one top level image directory in the mysql-docker repo.
// Dir doubles as the app name written under library/.
type MySQLDockerProduct struct {
	Dir string
	// VersionARG is the Dockerfile ARG carrying the exact version being installed,
	// eg: ARG MYSQL_ROUTER_PACKAGE=mysql-router-community-9.7.1
	VersionARG string
}

var mysqlDockerProducts = []MySQLDockerProduct{
	{Dir: "mysql-router", VersionARG: "MYSQL_ROUTER_PACKAGE"},
}

var (
	// series directories are named after the release series, eg: 8.0, 8.4, 9.7
	seriesRegex = regexp.MustCompile(`^\d+\.\d+$`)
	// LATEST="9.7" in mysql-<product>/VERSION
	latestRegex = regexp.MustCompile(`(?m)^LATEST=['"]?([\d.]+)['"]?`)
	// LATEST_LTS="9.7" in mysql-<product>/VERSION
	latestLTSRegex = regexp.MustCompile(`(?m)^LATEST_LTS=['"]?([\d.]+)['"]?`)
)

func ProcessMySQLDockerRepo(apps map[string]api.AppHistory) error {
	klog.InfoS("git clone", "repo", MySQLDockerRepo)
	r, err := git.Clone(memory.NewStorage(), nil, &git.CloneOptions{
		URL: MySQLDockerRepo,
	})
	if err != nil {
		return errors.Wrap(err, "repo: "+MySQLDockerRepo)
	}

	ref, err := r.Head()
	if err != nil {
		return err
	}
	head, err := r.CommitObject(ref.Hash())
	if err != nil {
		return err
	}
	tree, err := head.Tree()
	if err != nil {
		return err
	}

	for _, p := range mysqlDockerProducts {
		if skipApps.Has(p.Dir) {
			klog.InfoS("skipping", "app", p.Dir)
			continue
		}

		app, err := p.ParseApp(r, head, tree)
		if err != nil {
			return errors.Wrap(err, "app: "+p.Dir)
		}
		klog.InfoS("processed", "commit", head.Hash, "app", p.Dir, "blocks", len(app.Blocks))

		h, found := apps[app.Name]
		if !found {
			h = api.AppHistory{
				Name:      app.Name,
				GitRepo:   app.GitRepo,
				KnownTags: sets.NewString(),
				Blocks:    nil,
			}
		}
		GatherHistory(&h, app)
		apps[app.Name] = h
	}

	return nil
}

// ParseApp turns every release series directory of a product into Blocks, newest
// series first, so that the floating tags land on the newest series.
//
// A series directory only ever holds the current patch release, so the history of
// each Dockerfile is walked to keep the tags of superseded releases alive. Without
// that, upgrading a series would drop the tag that build_tags.txt still builds.
func (p MySQLDockerProduct) ParseApp(r *git.Repository, head *object.Commit, tree *object.Tree) (*api.App, error) {
	latest, latestLTS, err := p.ParseVersionFile(tree)
	if err != nil {
		return nil, err
	}

	series, err := p.ListSeries(tree)
	if err != nil {
		return nil, err
	}

	app := api.App{
		Name:    p.Dir,
		GitRepo: MySQLDockerRepo,
	}
	for _, s := range series {
		blocks, err := p.ParseSeries(r, head, s, s == latest, s == latestLTS)
		if err != nil {
			return nil, errors.Wrap(err, "series: "+s)
		}
		app.Blocks = append(app.Blocks, blocks...)
	}
	return &app, nil
}

// ParseSeries walks the history of a single series Dockerfile, newest commit first,
// and emits one Block per release it pinned. Each Block is pinned to the commit that
// introduced that release rather than to HEAD, so an unrelated release does not
// rebuild every series.
func (p MySQLDockerProduct) ParseSeries(r *git.Repository, head *object.Commit, series string, isLatest, isLatestLTS bool) ([]api.Block, error) {
	dockerfile := path.Join(p.Dir, series, "Dockerfile")

	iter, err := r.Log(&git.LogOptions{
		From:       head.Hash,
		Order:      git.LogOrderCommitterTime,
		PathFilter: func(p string) bool { return p == dockerfile },
	})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var blocks []api.Block
	current := true
	err = iter.ForEach(func(c *object.Commit) error {
		content, err := ReadCommitFile(c, dockerfile)
		if err != nil {
			// the Dockerfile was deleted, or did not exist yet, at this commit
			return nil
		}
		version, err := p.ParseVersion(content)
		if err != nil {
			// older layouts pinned the version some other way
			return nil
		}

		// The series, latest and lts tags float, so they belong to the release the
		// series currently points at.
		tags := []string{version}
		if current {
			tags = append(tags, series)
			if isLatest {
				tags = append(tags, "latest")
			}
			if isLatestLTS {
				tags = append(tags, "lts")
			}
			current = false
		}

		blocks = append(blocks, api.Block{
			Tags: tags,
			Architectures: map[string]*api.ArchInfo{
				"amd64":   {Architecture: "amd64"},
				"arm64v8": {Architecture: "arm64v8"},
			},
			GitCommit: c.Hash.String(),
			Directory: path.Join(p.Dir, series),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(blocks) == 0 {
		return nil, errors.New("no release found in " + dockerfile)
	}
	return blocks, nil
}

// ParseVersionFile reads the series carrying the latest and the latest LTS release
// out of mysql-<product>/VERSION.
func (p MySQLDockerProduct) ParseVersionFile(tree *object.Tree) (string, string, error) {
	filename := path.Join(p.Dir, "VERSION")
	content, err := ReadFile(tree, filename)
	if err != nil {
		return "", "", err
	}

	var latest, latestLTS string
	if m := latestRegex.FindStringSubmatch(content); len(m) > 1 {
		latest = m[1]
	}
	if m := latestLTSRegex.FindStringSubmatch(content); len(m) > 1 {
		latestLTS = m[1]
	}
	if latest == "" {
		return "", "", errors.New("missing LATEST in " + filename)
	}
	return latest, latestLTS, nil
}

// ListSeries returns the release series directories of a product, newest first.
func (p MySQLDockerProduct) ListSeries(tree *object.Tree) ([]string, error) {
	sub, err := tree.Tree(p.Dir)
	if err != nil {
		return nil, errors.Wrap(err, "dir: "+p.Dir)
	}

	series := make([]string, 0, len(sub.Entries))
	for _, entry := range sub.Entries {
		if entry.Mode == filemode.Dir && seriesRegex.MatchString(entry.Name) {
			series = append(series, entry.Name)
		}
	}
	if len(series) == 0 {
		return nil, errors.New("no release series found in " + p.Dir)
	}

	sort.Slice(series, func(i, j int) bool {
		vi, erri := semver.NewVersion(series[i])
		vj, errj := semver.NewVersion(series[j])
		if erri != nil || errj != nil {
			return series[i] > series[j]
		}
		return vi.GreaterThan(vj)
	})
	return series, nil
}

// ParseVersion pulls the exact version out of the Dockerfile ARG that pins the package,
// eg: 9.7.1 from ARG MYSQL_ROUTER_PACKAGE=mysql-router-community-9.7.1
func (p MySQLDockerProduct) ParseVersion(content string) (string, error) {
	re, err := regexp.Compile(`(?m)^ARG\s+` + regexp.QuoteMeta(p.VersionARG) + `=\S*?(\d+\.\d+\.\d+)\s*$`)
	if err != nil {
		return "", err
	}
	m := re.FindStringSubmatch(content)
	if len(m) < 2 {
		return "", errors.New("missing ARG " + p.VersionARG)
	}
	return m[1], nil
}

func ReadFile(tree *object.Tree, filename string) (string, error) {
	f, err := tree.File(filename)
	if err != nil {
		return "", errors.Wrap(err, "file: "+filename)
	}
	return f.Contents()
}

func ReadCommitFile(c *object.Commit, filename string) (string, error) {
	f, err := c.File(filename)
	if err != nil {
		return "", errors.Wrap(err, "file: "+filename)
	}
	return f.Contents()
}
