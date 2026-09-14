package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/bketelsen/adc/internal/adc"
)

func serve() error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	publicContributions := flags.Bool("public-contributions", false, "enable deliberately public contribution queue endpoints")
	addr := flags.String("addr", "127.0.0.1:8789", "HTTP listen address")
	dir := flags.String("data", ".adc", "Private application data directory")
	secure := flags.Bool("secure-cookies", false, "Use with an HTTPS reverse proxy")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return err
	}
	store, err := adc.Open(*dir)
	if err != nil {
		return err
	}
	defer store.Close()
	lock, err := os.OpenFile(filepath.Join(store.Dir, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return fmt.Errorf("another ADC service owns this data directory")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	listener, err := net.Listen("tcp", *addr)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	engine := adc.NewEngine(store)
	engine.Start(ctx)
	defer engine.Stop()
	web := adc.NewWeb(store, engine, *secure)
	web.PublicContributions = *publicContributions
	server := &http.Server{Handler: web.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 90 * time.Second, MaxHeaderBytes: 1 << 20}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	fmt.Printf("Aide de Camp listening at http://%s\n", listener.Addr())
	err = server.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
