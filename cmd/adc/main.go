package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/bketelsen/adc/internal/adc"
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
	if len(os.Args) > 1 && os.Args[1] == "contribution-runtime" {
		flags := flag.NewFlagSet("contribution-runtime", flag.ContinueOnError)
		source := flags.String("source", "", "curated public runtime directory to validate and fingerprint")
		if err := flags.Parse(os.Args[2:]); err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		id, err := adc.ContributionRuntimeID(ctx, *source)
		if err == nil {
			fmt.Println(id)
		}
		return err
	}
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		return serve()
	}
	if len(os.Args) > 1 && os.Args[1] == "prove" {
		return prove()
	}
	if len(os.Args) < 2 || os.Args[1] != "doctor" {
		return fmt.Errorf("usage: adc serve [-addr address] [-data directory] | doctor | prove | contribution-runtime -source directory")
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
