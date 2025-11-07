package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/rs/zerolog"
	"github.com/trains-io/z21.go"
)

type Config struct {
	Z21Name           string
	Z21Addr           string
	NATSURL           string
	HeartbeatInterval time.Duration
	Logger            zerolog.Logger
}

var usageStr = `The z21-gateway is a lightweight gateway application that bridges a z21 device
to a NATS message bus.

Usage: z21-gateway [options]

Gateway Options:
	-zc, --z21_addr <host[:port]>  z21 address (default: 127.0.0.1:21105)
	-nc, --nats_url <host>         NATS server URL (default: nats://127.0.0.1:4222)
	-n, --name
	    --z21_name <z21_name>      z21 name (default: main)

Environment Variables:
	Z21_NAME (overridden by --z21_name)
	Z21_ADDR (overridden by --z21_addr)
	NATS_URL (overridden by --nats_url)
`

func LoadConfig() Config {
	defaultZ21Name := getenv("Z21_NAME", z21.DefaultName)
	defaultZ21Addr := getenv("Z21_ADDR", z21.DefaultURL)
	defaultNATSURL := getenv("NATS_URL", nats.DefaultURL)

	var (
		z21Name string
		z21Addr string
		natsURL string
	)

	flag.StringVar(&z21Name, "z21_name", defaultZ21Name, "Z21 name")
	flag.StringVar(&z21Name, "n", defaultZ21Name, "Z21 name (shorthand)")

	flag.StringVar(&z21Addr, "z21_addr", defaultZ21Addr, "Z21 address")
	flag.StringVar(&z21Addr, "zc", defaultZ21Addr, "Z21 address (shorthand)")

	flag.StringVar(&natsURL, "nats_url", defaultNATSURL, "NATS server URL")
	flag.StringVar(&natsURL, "nc", defaultNATSURL, "NATS server URL (shorthand)")

	flag.Usage = func() {
		fmt.Printf("%s\n", usageStr)
		os.Exit(0)
	}

	flag.Parse()

	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr}).
		Level(zerolog.DebugLevel).
		With().
		Str("component", "z21gw").
		Timestamp().
		Logger()

	return Config{
		Z21Name:           z21Name,
		Z21Addr:           z21Addr,
		NATSURL:           natsURL,
		HeartbeatInterval: 30 * time.Second,
		Logger:            logger,
	}
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
