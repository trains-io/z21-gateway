package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nats-io/nats.go"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Printf("%s version: %s %s %s\n", os.Args[0], version, commit, date)
		os.Exit(0)
	}
	cfg := LoadConfig()

	cfg.Logger.Info().Msg("starting Z21 Gateway")
	cfg.Logger.Info().
		Str("version", version).
		Str("sha", commit).
		Str("build", date).
		Str("context", cfg.Z21Name).
		Str("z21", cfg.Z21Addr).
		Str("nats", cfg.NATSURL).
		Msg("config")

	opts := []nats.Option{
		nats.Name("z21gw"),
		nats.DisconnectErrHandler(func(c *nats.Conn, err error) {
			cfg.Logger.Warn().
				Err(err).
				Str("status", "disconnected").
				Msg("NATS conn")
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			cfg.Logger.Info().
				Str("status", "reconnected").
				Msg("NATS conn")
		}),
	}
	nc, err := nats.Connect(cfg.NATSURL, opts...)
	if err != nil {
		cfg.Logger.Fatal().
			Err(err).
			Msg("NATS conn")
	}
	defer nc.Drain()
	cfg.Logger.Info().
		Str("url", cfg.NATSURL).
		Msg("NATS conn")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	gw, err := NewGateway(ctx, nc, cfg.Z21Name, cfg.Z21Addr, cfg.Logger)
	if err != nil {
		cfg.Logger.Fatal().
			Err(err).
			Msg("Z21 conn")
	}
	cfg.Logger.Info().
		Str("addr", cfg.Z21Addr).
		Str("context", cfg.Z21Name).
		Msg("Z21 conn")

	gw.Start()
	cfg.Logger.Info().Msg("Z21 Gateway started")

	<-ctx.Done()
	gw.Stop()

	cfg.Logger.Info().Msg("Z21 Gateway stopped cleanly")
}
