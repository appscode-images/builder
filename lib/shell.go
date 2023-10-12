package lib

import (
	shell "gomodules.xyz/go-sh"
	"os"
)

func NewShell() *shell.Session {
	sh := shell.NewSession()
	sh.SetDir("/tmp")
	sh.SetEnv("GITHUB_TOKEN", os.Getenv("GITHUB_TOKEN"))

	sh.ShowCMD = true
	sh.Stdout = os.Stdout
	sh.Stderr = os.Stderr
	return sh
}
