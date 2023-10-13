package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/appscode-images/builder/lib"
	"sigs.k8s.io/yaml"
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
		wfYAML = strings.ReplaceAll(wfYAML, "$tags$", strings.Join(tags, ", "))
		if err := os.WriteFile(wfFile, []byte(wfYAML), 0644); err != nil {
			return err
		}
	}
	return nil
}

func formatYAML(s string) (string, error) {
	m := map[string]any{}
	err := yaml.Unmarshal([]byte(s), &m)
	if err != nil {
		return "", err
	}
	data, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

const wf = `name: build-$name$

on:
  workflow_dispatch:

concurrency:
  group: ${{ github.workflow }}-${{ github.head_ref || github.ref }}-build-$name$
  cancel-in-progress: true

jobs:
  build:
    name: Build
    runs-on: ubuntu-latest
    strategy:
      matrix:
        tag: [$tags$]
    steps:
    - uses: actions/checkout@v3

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

    - name: Install crane
      run: |
        # VERSION=$(curl -s "https://api.github.com/repos/google/go-containerregistry/releases/latest" | jq -r '.tag_name')
        # OS=Linux
        # ARCH=x86_64
        # curl -sL "https://github.com/google/go-containerregistry/releases/download/${VERSION}/go-containerregistry_${OS}_${ARCH}.tar.gz" > go-containerregistry.tar.gz
        # tar -zxvf go-containerregistry.tar.gz -C /usr/local/bin/ crane
        cd ..
        git clone https://github.com/gomodules/go-containerregistry.git
        cd go-containerregistry
        git checkout mutate-manifest
        GOBIN=/usr/local/bin go install -v ./...

    - name: Install trivy
      run: |
        # wget https://github.com/aquasecurity/trivy/releases/download/v0.18.3/trivy_0.18.3_Linux-64bit.deb
        # sudo dpkg -i trivy_0.18.3_Linux-64bit.deb
        sudo apt-get install -y --no-install-recommends wget apt-transport-https gnupg lsb-release
        wget -qO - https://aquasecurity.github.io/trivy-repo/deb/public.key | sudo apt-key add -
        echo deb https://aquasecurity.github.io/trivy-repo/deb $(lsb_release -sc) main | sudo tee -a /etc/apt/sources.list.d/trivy.list
        sudo apt-get update
        sudo apt-get install -y --no-install-recommends trivy

    # - name: Setup upterm session
    #   uses: lhotari/action-upterm@v1

    - name: Build
      run: |
        go run cmd/build-image/main.go --name=$name$ --tag=${{ matrix.tag }}

  report:
    name: Report
    runs-on: ubuntu-latest
    needs: build
    steps:
    - uses: actions/checkout@v3

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
        # https://stackoverflow.com/a/23930212
        cat > ./library/$name$/README.md <<- EOM
        # $name$
        Last Updated: $(date --rfc-3339=date)
        EOM

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
