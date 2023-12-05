package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/appscode-images/builder/lib"
	flag "github.com/spf13/pflag"
)

func main() {
	var name = flag.String("name", "elastic", "Name of binary")
	flag.Parse()

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	apptxt := filepath.Join(dir, "library", *name, "app.txt")
	app, err := lib.ParseLibraryFile(apptxt)
	if err != nil {
		panic(err)
	}

	data, err := json.MarshalIndent(app, "", "  ")
	if err != nil {
		panic(err)
	}
	appjson := filepath.Join(dir, "library", *name, "app.json")
	err = os.WriteFile(appjson, data, 0644)
	if err != nil {
		panic(err)
	}
}
