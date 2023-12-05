package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appscode-images/builder/lib"
)

func main() {
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
		if strings.HasPrefix(entry.Name(), "build-") {
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
	case "node":
		return "firecracker"
	default:
		return "ubuntu-latest"
	}
}

const wf = `name: build-$name$

on:
  # schedule:
  #   - cron: '0 1 * * *'
  workflow_dispatch:

concurrency:
  group: ${{ github.workflow }}-${{ github.head_ref || github.ref }}
  cancel-in-progress: true

jobs:
  build:
    name: Build
    runs-on: $runner$
    strategy:
      fail-fast: false
      matrix:
        tag: [$tags$]
    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Prepare git
      env:
        GITHUB_USER: 1gtm
        GITHUB_TOKEN: ${{ secrets.LGTM_GITHUB_TOKEN }}
      run: |
        set -x
        git config --global user.name "1gtm"
        git config --global user.email "1gtm@appscode.com"
        git config --global \
          url."https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com".insteadOf \
          "https://github.com"
        # git remote set-url origin https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git

    - name: Set up QEMU
      id: qemu
      uses: docker/setup-qemu-action@v3

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3
      with:
        platforms: linux/amd64,linux/arm64

    - uses: imjasonh/setup-crane@v0.1

    - name: Install trivy
      run: |
        # wget https://github.com/aquasecurity/trivy/releases/download/v0.18.3/trivy_0.18.3_Linux-64bit.deb
        # sudo dpkg -i trivy_0.18.3_Linux-64bit.deb
        sudo apt-get install -y --no-install-recommends wget apt-transport-https gnupg lsb-release
        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
        echo deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main | sudo tee -a /etc/apt/sources.list.d/trivy.list
        sudo apt-get update
        sudo apt-get install -y --no-install-recommends trivy

    - name: Log in to the GitHub Container registry
      uses: docker/login-action@v2
      with:
        registry: ghcr.io
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    # - name: Setup upterm session
    #   uses: lhotari/action-upterm@v1

    - name: Build
      run: |
        go run cmd/build-image/main.go --name=$name$ --tag=${{ matrix.tag }}

  report:
    name: Report
    runs-on: $runner$
    needs: build
    if: always()
    steps:
    - uses: actions/checkout@v4

    - name: Set up Go
      uses: actions/setup-go@v4
      with:
        go-version: '1.21'

    - name: Prepare git
      env:
        GITHUB_USER: 1gtm
        GITHUB_TOKEN: ${{ secrets.LGTM_GITHUB_TOKEN }}
      run: |
        set -x
        git config --global user.name "1gtm"
        git config --global user.email "1gtm@appscode.com"
        git config --global \
          url."https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com".insteadOf \
          "https://github.com"
        # git remote set-url origin https://${GITHUB_USER}:${GITHUB_TOKEN}@github.com/${GITHUB_REPOSITORY}.git

    - name: Set up QEMU
      id: qemu
      uses: docker/setup-qemu-action@v3

    - name: Set up Docker Buildx
      uses: docker/setup-buildx-action@v3
      with:
        platforms: linux/amd64,linux/arm64

    - name: Log in to the GitHub Container registry
      uses: docker/login-action@v2
      with:
        registry: ghcr.io
        username: ${{ github.actor }}
        password: ${{ secrets.GITHUB_TOKEN }}

    - name: Install trivy
      run: |
        # wget https://github.com/aquasecurity/trivy/releases/download/v0.18.3/trivy_0.18.3_Linux-64bit.deb
        # sudo dpkg -i trivy_0.18.3_Linux-64bit.deb
        sudo apt-get install -y --no-install-recommends wget apt-transport-https gnupg lsb-release
        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
        echo deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main | sudo tee -a /etc/apt/sources.list.d/trivy.list
        sudo apt-get update
        sudo apt-get install -y --no-install-recommends trivy

    - name: Build
      env:
        SMTP_ADDRESS: ${{ secrets.SMTP_ADDRESS }}
        SMTP_USERNAME: ${{ secrets.SMTP_USERNAME }}
        SMTP_PASSWORD: ${{ secrets.SMTP_PASSWORD }}
      run: |
        go run cmd/mail-report/main.go --name=$name$

    - name: Update repo
      run: |
        git add --all
        if [[ $(git status --porcelain) ]]; then
          git commit -s -a -m "update $name$ images $(date --rfc-3339=date)"
          git fetch origin
          git pull --rebase origin master
          git push origin HEAD
        fi
`
