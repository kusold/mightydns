package main

import (
	"fmt"
	"os"

	"github.com/kusold/mightydns"
	_ "github.com/kusold/mightydns/module/dns"
	_ "github.com/kusold/mightydns/module/dns/resolver"
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

	var configData []byte
	var err error

	// Check if a config file is provided
	if len(os.Args) > 2 && os.Args[1] == "--config" {
		configFile := os.Args[2]
		// #nosec G304 - intentionally reading user-specified config file
		configData, err = os.ReadFile(configFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error reading config file %s: %v\n", configFile, err)
			os.Exit(1)
		}

		// Load the provided config
		err = mightydns.Load(configData, true)
	} else {
		// Use default config (Run with nil creates default)
		err = mightydns.Run(nil)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Keep the server running
	select {}
}
