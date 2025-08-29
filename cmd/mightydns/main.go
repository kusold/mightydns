package main

import (
	"fmt"
	"os"

	"github.com/kusold/mightydns"
	_ "github.com/kusold/mightydns/module/log/handler"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "list-modules" {
		modules := mightydns.GetModules()
		fmt.Println("Registered modules:")
		for id, info := range modules {
			fmt.Printf("  %s\n", id)
			_ = info
		}
		os.Exit(0)
	}

	err := mightydns.Run(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
