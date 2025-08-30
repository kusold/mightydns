package main

import (
	"context"
	"fmt"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/kusold/mightydns"
)

func main() {
	app := &cli.Command{
		Name:    "mightydns",
		Usage:   "A modular DNS server",
		Version: "dev",
		Commands: []*cli.Command{
			{
				Name:  "run",
				Usage: "Start the DNS server",
				Flags: []cli.Flag{
					&cli.StringFlag{
						Name:    "config",
						Aliases: []string{"c"},
						Usage:   "Load configuration from `FILE`",
					},
				},
				Action: runServer,
			},
			{
				Name:   "list-modules",
				Usage:  "List all registered modules",
				Action: listModules,
			},
		},
		DefaultCommand: "run",
	}

	if err := app.Run(context.Background(), os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runServer(ctx context.Context, cmd *cli.Command) error {
	configFile := cmd.String("config")

	if configFile != "" {
		// #nosec G304 - intentionally reading user-specified config file
		configData, err := os.ReadFile(configFile)
		if err != nil {
			return fmt.Errorf("reading config file %s: %w", configFile, err)
		}

		// Load the provided config
		if err := mightydns.Load(configData, true); err != nil {
			return err
		}
	} else {
		// Use default config (Run with nil creates default)
		if err := mightydns.Run(nil); err != nil {
			return err
		}
	}

	// Keep the server running
	select {}
}

func listModules(ctx context.Context, cmd *cli.Command) error {
	modules := mightydns.GetModules()
	fmt.Println("Registered modules:")
	for id := range modules {
		fmt.Printf("  %s\n", id)
	}
	return nil
}
