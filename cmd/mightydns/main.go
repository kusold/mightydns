package main

import (
	"os"

	"github.com/kusold/mightydns"
)

func main() {
	mightydns.Run(nil)
	os.Exit(0)
}
