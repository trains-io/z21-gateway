package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog"
	"github.com/trains-io/z21.go"

	"github.com/nats-io/nats.go"
)

const (
	HeartbeatInterval     = 20 * time.Second
	RequestTimeout        = 500 * time.Millisecond
	MaxConcurrentCommands = 4
)

type Gateway struct {
	name   string
	zc     *z21.Conn
	nc     *nats.Conn
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
	logger zerolog.Logger
	sem    chan struct{}
}

type StatusMsg struct {
	Reachable bool   `json:"reachable"`
	Serial    string `json:"serial:omitempty"`
	TS        string `json:"ts"`
}

type CmdRequest struct {
	Type string           `json:"type"`
	Data z21.Serializable `json:"request,omitempty"`
}

type CmdReply struct {
	Type  string           `json:"type"`
	Ok    bool             `json:"ok"`
	Data  z21.Serializable `json:"reply,omitempty"`
	Error string           `json:"error,omitempty"`
	TS    string           `json:"ts"`
}

func NewGateway(ctx context.Context, nc *nats.Conn, name, addr string, logger zerolog.Logger) (*Gateway, error) {
	zc, err := z21.Connect(addr, z21.Verbose(true))
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithCancel(ctx)
	return &Gateway{
		name:   name,
		zc:     zc,
		nc:     nc,
		ctx:    cctx,
		cancel: cancel,
		logger: logger,
		sem:    make(chan struct{}, MaxConcurrentCommands),
	}, nil
}

func (g *Gateway) Start() error {
	g.logger.Debug().
		Str("component", "z21gw").
		Msg("starting Z21 heartbeat loop")
	g.wg.Add(1)
	go g.heartbeatLoop()

	g.logger.Debug().
		Str("component", "z21gw").
		Msg("starting Z21 events loop")
	g.wg.Add(1)
	go g.z21EventsLoop()

	g.logger.Debug().
		Str("component", "z21gw").
		Msg("Z21 broadcast sub")
	g.subscribeBroadcast()

	g.logger.Debug().
		Str("component", "z21gw").
		Msg("starting NATS commands loop")
	if err := g.natsCommandsLoop(); err != nil {
		return err
	}

	return nil
}

func (g *Gateway) Stop() {
	g.cancel()
	g.zc.Close()
	g.wg.Wait()
	g.nc.Flush()
}

func (g *Gateway) heartbeatLoop() {
	defer g.wg.Done()
	ticker := time.NewTicker(HeartbeatInterval)
	defer ticker.Stop()

	g.doHeartbeatCheck()

	for {
		select {
		case <-g.ctx.Done():
			return
		case <-ticker.C:
			g.doHeartbeatCheck()
		}
	}
}

func (g *Gateway) doHeartbeatCheck() {
	status := g.checkReachability()

	data, err := json.Marshal(status)
	if err != nil {
		g.logger.Error().
			Err(err).
			Str("component", "z21gw").
			Msg("failed to marshall status request")
		return
	}

	subject := fmt.Sprintf("z21.%s.status", g.name)
	if err := g.nc.Publish(subject, data); err != nil {
		g.logger.Error().
			Err(err).
			Str("component", "z21gw").
			Msg("failed to publish heartbeat status")
		return
	}
	g.logger.Info().
		Str("component", "z21gw").
		Str("subject", subject).
		Bool("reachable", status.Reachable).
		Str("serial", status.Serial).
		Msg("NATS pub")
}

func (g *Gateway) checkReachability() *StatusMsg {
	ctx, cancel := context.WithTimeout(g.ctx, RequestTimeout)
	defer cancel()

	reachable := false
	serial := ""

	g.logger.Debug().
		Str("component", "z21gw").
		Msg("sending hearbeat")

	msg, err := g.zc.SendRcv(ctx, &z21.SerialNumber{})
	if err == nil {
		if sn, ok := msg.(*z21.SerialNumber); ok {
			reachable = true
			serial = fmt.Sprintf("%d", sn.SerialNumber)
		}
	}

	return &StatusMsg{
		Reachable: reachable,
		Serial:    serial,
		TS:        time.Now().UTC().Format(time.RFC3339),
	}
}

func (g *Gateway) z21EventsLoop() {
	defer g.wg.Done()
	events := g.zc.Events()

	for {
		select {
		case <-g.ctx.Done():
			return
		case ev := <-events:
			g.publishEvent(ev)
		}
	}
}

func (g *Gateway) publishEvent(ev z21.Serializable) {
	data, err := json.Marshal(ev)
	if err != nil {
		g.logger.Error().
			Err(err).
			Str("component", "z21gw").
			Msg("failed to marshall event")
		return
	}

	subject := fmt.Sprintf("z21.%s.event.%s", g.name, ev)
	if err := g.nc.Publish(subject, data); err != nil {
		g.logger.Error().
			Err(err).
			Str("component", "z21gw").
			Msg("failed to publish")
		return
	}

	g.logger.Info().
		Str("component", "z21gw").
		Str("subject", subject).
		Msg("NATS pub")
}

func (g *Gateway) subscribeBroadcast() {
	ctx := context.Background()
	_, err := g.zc.SendRcv(ctx, &z21.BroadcastFlags{Flags: z21.Mask32(z21.SYSTEM_UPDATES)})
	if err != nil {
		g.logger.Error().
			Str("component", "z21gw").
			Err(err)
	}
}

func (g *Gateway) natsCommandsLoop() error {
	subject := fmt.Sprintf("z21.%s.cmd", g.name)
	_, err := g.nc.Subscribe(subject, func(m *nats.Msg) {
		go g.handleCmdMessage(m)
	})
	if err != nil {
		return err
	}

	g.logger.Info().
		Str("component", "z21gw").
		Str("subject", subject).
		Msg("NATS sub")

	return nil
}

func (g *Gateway) handleCmdMessage(msg *nats.Msg) {
	select {
	case g.sem <- struct{}{}:
		defer func() { <-g.sem }()
	case <-g.ctx.Done():
		return
	}

	reply := g.doCmdRequest(msg)
	data, err := json.Marshal(reply)
	if err != nil {
		g.logger.Error().
			Str("component", "z21gw").
			Err(err).
			Msg("NATS msg")
		return
	}

	var subject string
	// publish to NATS internal request-reply topic
	if msg.Reply != "" {
		subject = msg.Reply
	} else {
		// fallback to generic topic
		subject = fmt.Sprintf("z21.%s.reply", g.name)
	}
	if err := g.nc.Publish(subject, data); err != nil {
		g.logger.Error().
			Str("component", "z21gw").
			Err(err).
			Msg("NATS msg")
	}

	g.logger.Info().
		Str("component", "z21gw").
		Bool("ok", reply.Ok).
		Msg("NATS msg")
}

func (g *Gateway) doCmdRequest(msg *nats.Msg) CmdReply {
	var req CmdRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		g.logger.Error().
			Str("component", "z21gw").
			Err(err).
			Msg("NATS msg")

		return CmdReply{
			Ok:    false,
			Error: fmt.Sprintf("invalid message: %s", err),
			TS:    time.Now().Format(time.RFC3339),
		}
	}

	g.logger.Info().
		Str("component", "z21gw").
		Str("type", req.Type).
		Msg("NATS message")

	ctx, cancel := context.WithTimeout(g.ctx, RequestTimeout)
	defer cancel()

	g.logger.Debug().
		Str("component", "z21gw").
		Msg("Z21 tx")

	resp, err := g.zc.SendRcv(ctx, req.Data)
	reply := CmdReply{
		Type: req.Type,
		TS:   time.Now().Format(time.RFC3339),
	}
	if err != nil {
		g.logger.Error().
			Str("component", "z21gw").
			Err(err).
			Msg("Z21 rx")
		reply.Ok = false
		reply.Error = fmt.Sprintf("%s", err)
	} else {
		g.logger.Debug().
			Str("component", "z21gw").
			Msg("Z21 rx")
		reply.Ok = true
		reply.Data = resp
	}
	return reply
}
