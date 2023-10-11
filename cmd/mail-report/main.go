package main

import (
	"fmt"
	flag "github.com/spf13/pflag"
)

func main() {
	var name *string = flag.String("name", "", "Name of binary")

	flag.Parse()

	fmt.Println("EMAIL BUILD REPORT for " + *name)
}
