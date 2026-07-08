package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appscode-images/builder/lib"
	flag "github.com/spf13/pflag"
	"gomodules.xyz/sets"
	"k8s.io/klog/v2"
)

var skipApps sets.String

func main() {
	skipList := []string{
		"elastic",
		"ferretdb",
		"kibana",
		"milvus",
		"opensearch",
		"opensearch-dashboards",
		"qdrant",
	}
	flag.StringSliceVar(&skipList, "skip", skipList, "Skip official image (because manually maintained)")
	flag.Parse()

	skipApps = sets.NewString(skipList...)

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	if err := CleanupOldWorkflows(dir); err != nil {
		panic(err)
	}
	if err := GenerateWorkflows(dir); err != nil {
		panic(err)
	}
}

func CleanupOldWorkflows(dir string) error {
	wfDir := filepath.Join(dir, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if after, ok := strings.CutPrefix(entry.Name(), "build-"); ok {
			appName := strings.TrimSuffix(after, filepath.Ext(entry.Name()))
			if skipApps.Len() > 0 && skipApps.Has(appName) {
				continue
			}

			if err = os.Remove(filepath.Join(wfDir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}

func GenerateWorkflows(dir string) error {
	libDir := filepath.Join(dir, "library")
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		appName := entry.Name()
		if skipApps.Len() > 0 && skipApps.Has(appName) {
			klog.InfoS("skipping", "app", appName)
			continue
		}

		tags, err := lib.ListBuildTags(dir, entry.Name())
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		if len(tags) == 0 {
			continue
		}

		wfDir := filepath.Join(dir, ".github", "workflows")
		if err := os.MkdirAll(wfDir, 0755); err != nil {
			return err
		}
		wfFile := filepath.Join(wfDir, fmt.Sprintf("build-%s.yml", entry.Name()))

		wfYAML := strings.ReplaceAll(wf, "$name$", entry.Name())
		wfYAML = strings.ReplaceAll(wfYAML, "$runner$", selectRunner(entry.Name()))
		wfYAML = strings.ReplaceAll(wfYAML, "$tags$", strings.Join(tags, ", "))
		if err := os.WriteFile(wfFile, []byte(wfYAML), 0644); err != nil {
			return err
		}
	}
	return nil
}

func selectRunner(name string) string {
	switch name {
	case "node", "postgres", "memcached", "solr":
		return "firecracker"
	default:
		return "ubuntu-latest"
	}
}

const wf = `name: build-$name$

on:
  schedule:
    - cron: '0 0 */14 * *'
  workflow_dispatch:

concurrency:
  group: ${{ github.workflow }}-${{ github.head_ref || github.ref }}
  cancel-in-progress: true

jobs:
  build:
    name: Build
    runs-on: $runner$
    permissions:
      packages: write
      contents: write
    strategy:
      fail-fast: false
      matrix:
        tag: [$tags$]
    steps:
    - uses: actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5 # v4.3.1

    - name: Set up Go
      uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0
      with:
        go-version: '1.25'

    - name: Generate LGTM App token
      id: lgtm-app-token
      uses: actions/create-github-app-token@1b10c78c7865c340bc4f6099eb2f838309f1e8c3 # v3
      with:
        permission-contents: write
        client-id: ${{ secrets.LGTM_APP_CLIENT_ID }}
        private-key: ${{ secrets.LGTM_APP_PRIVATE_KEY }}
        owner: ${{ github.repository_owner }}

    - name: Prepare git
      env:
        GITHUB_USER: 1gtm
        GITHUB_TOKEN: ${{ steps.lgtm-app-token.outputs.token }}
        # GITHUB_TOKEN: ${{ secrets.LGTM_GITHUB_TOKEN }}
      run: |
        set -x
        git config --global user.name "1gtm"
        git config --global user.email "1gtm@appscode.com"
        git config --global \
          url."https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com".insteadOf \
          "https://github.com"
        git remote set-url origin https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git

    - name: Set up QEMU
      id: qemu
      uses: docker/setup-qemu-action@c7c53464625b32c7a7e944ae62b3e17d2b600130 # v3.7.0
      with:
        cache-image: false

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@8d2750c68a42422c14e847fe6c8ac0403b4cbd6f # v3.12.0
      with:
        platforms: linux/amd64,linux/arm64

    - uses: imjasonh/setup-crane@5146f708a817ea23476677995bf2133943b9be0b # v0.1

    - name: Install trivy
      run: |
        # wget https://github.com/aquasecurity/trivy/releases/download/v0.18.3/trivy_0.18.3_Linux-64bit.deb
        # sudo dpkg -i trivy_0.18.3_Linux-64bit.deb
        sudo apt-get install -y --no-install-recommends wget apt-transport-https gnupg lsb-release
        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | gpg --dearmor | sudo tee /usr/share/keyrings/trivy.gpg > /dev/null
        echo "deb [signed-by=/usr/share/keyrings/trivy.gpg] https://aquasecurity.github.io/trivy-repo/deb generic main" | sudo tee -a /etc/apt/sources.list.d/trivy.list
        sudo apt-get update
        sleep 5
        sudo apt-get install -y --no-install-recommends trivy

    - name: Log in to the GitHub Container registry
      uses: docker/login-action@4907a6ddec9925e35a0a9e82d7399ccc52663121 # v4.1.0
      with:
        registry: ghcr.io
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    # - name: Setup upterm session
    #   uses: lhotari/action-upterm@v1

    - name: Build
      run: |
        go run cmd/build-image/main.go --name=$name$ --tag=${{ matrix.tag }}

#  report:
#    name: Report
#    runs-on: $runner$
#    needs: build
#    if: always()
#    steps:
#    - uses: actions/checkout@34e114876b0b11c390a56381ad16ebd13914f8d5 # v4.3.1
#
#    - name: Set up Go
#      uses: actions/setup-go@40f1582b2485089dde7abd97c1529aa768e1baff # v5.6.0
#      with:
#        go-version: '1.25'
#
#    - name: Prepare git
#      env:
#        GITHUB_USER: 1gtm
#        GITHUB_TOKEN: ${{ secrets.LGTM_GITHUB_TOKEN }}
#      run: |
#        set -x
#        git config --global user.name "1gtm"
#        git config --global user.email "1gtm@appscode.com"
#        git config --global \
#          url."https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com".insteadOf \
#          "https://github.com"
#        # git remote set-url origin https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git
#
#    - name: Set up QEMU
#      id: qemu
#      uses: docker/setup-qemu-action@c7c53464625b32c7a7e944ae62b3e17d2b600130 # v3.7.0
#      with:
#        cache-image: false
#
#    - name: Set up Docker Buildx
#      uses: docker/setup-buildx-action@8d2750c68a42422c14e847fe6c8ac0403b4cbd6f # v3.12.0
#      with:
#        platforms: linux/amd64,linux/arm64
#
#    - name: Log in to the GitHub Container registry
#      uses: docker/login-action@4907a6ddec9925e35a0a9e82d7399ccc52663121 # v4.1.0
#      with:
#        registry: ghcr.io
#        username: ${{ github.actor }}
#        password: ${{ secrets.GITHUB_TOKEN }}
#
#    - name: Install trivy
#      run: |
#        # wget https://github.com/aquasecurity/trivy/releases/download/v0.18.3/trivy_0.18.3_Linux-64bit.deb
#        # sudo dpkg -i trivy_0.18.3_Linux-64bit.deb
#        sudo apt-get install -y --no-install-recommends wget apt-transport-https gnupg lsb-release
#        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
#        echo deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main | sudo tee -a /etc/apt/sources.list.d/trivy.list
#        sudo apt-get update
#        sudo apt-get install -y --no-install-recommends trivy
#
#    - name: Build
#      env:
#        SMTP_ADDRESS: ${{ secrets.SMTP_ADDRESS }}
#        SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
#        SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
#      run: |
#        go run cmd/mail-report/main.go --name=$name$
#
#    - name: Update repo
#      run: |
#        git add --all
#        if [[ $(git status --porcelain) ]]; then
#          git commit -s -a -m "update $name$ images $(date --rfc-3339=date)"
#          git fetch origin
#          # https://git-scm.com/docs/merge-strategies
#          git pull --rebase -s ours origin master
#          git push origin HEAD
#        fi
`
