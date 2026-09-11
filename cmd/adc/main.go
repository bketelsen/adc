package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		return serve()
	}
	if len(os.Args) > 1 && os.Args[1] == "prove" {
		return prove()
	}
	if len(os.Args) < 2 || os.Args[1] != "doctor" {
		return fmt.Errorf("usage: adc serve [-addr address] [-data directory] | doctor | prove")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	c := copilot.NewClient(&copilot.ClientOptions{LogLevel: "error"})
	if err := c.Start(ctx); err != nil {
		return err
	}
	defer c.Stop()
	auth, err := c.GetAuthStatus(ctx)
	if err != nil {
		return err
	}
	models, err := c.ListModels(ctx)
	if err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]any{"auth": auth, "models": models})
}
