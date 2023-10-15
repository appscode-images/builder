package lib

import (
	"os"

	shell "gomodules.xyz/go-sh"
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
