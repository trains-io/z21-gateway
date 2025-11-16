package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
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
	name         string
	zc           *z21.Conn
	nc           *nats.Conn
	ctx          context.Context
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	logger       zerolog.Logger
	sem          chan struct{}
	onlineStatus chan bool
	isOnline     atomic.Bool
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
		name:         name,
		zc:           zc,
		nc:           nc,
		ctx:          cctx,
		cancel:       cancel,
		logger:       logger,
		sem:          make(chan struct{}, MaxConcurrentCommands),
		onlineStatus: make(chan bool, 1),
	}, nil
}

func (g *Gateway) Start() error {
	g.logger.Debug().
		Msg("starting Z21 heartbeat loop")
	g.wg.Add(1)
	go g.heartbeatLoop()

	g.logger.Debug().
		Msg("starting Z21 online status monitor")
	g.wg.Add(1)
	go g.monitorOnlineStatus()

	g.logger.Debug().
		Msg("starting Z21 events loop")
	g.wg.Add(1)
	go g.z21EventsLoop()

	g.logger.Debug().
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
	wasOnline := g.isOnline.Load()

	if status.Reachable != wasOnline {
		g.isOnline.Store(status.Reachable)

		select {
		case g.onlineStatus <- status.Reachable:
		default:
		}
	}

	data, err := json.Marshal(status)
	if err != nil {
		g.logger.Error().
			Err(err).
			Msg("failed to marshall status request")
		return
	}

	subject := fmt.Sprintf("z21.%s.status", g.name)
	if err := g.nc.Publish(subject, data); err != nil {
		g.logger.Error().
			Err(err).
			Msg("failed to publish heartbeat status")
		return
	}
	g.logger.Info().
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

func (g *Gateway) monitorOnlineStatus() {
	defer g.wg.Done()

	for {
		select {
		case <-g.ctx.Done():
			return
		case isOnline := <-g.onlineStatus:
			if isOnline {
				g.logger.Info().
					Msg("Z21 is ONLINE — sending broadcast subscription")
				g.subscribeBroadcast()
			} else {
				g.logger.Warn().
					Msg("Z21 is OFFLINE")
			}
		}
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
			Msg("failed to marshall event")
		return
	}

	subject := fmt.Sprintf("z21.%s.event.%s", g.name, ev)
	if err := g.nc.Publish(subject, data); err != nil {
		g.logger.Error().
			Err(err).
			Msg("failed to publish")
		return
	}

	g.logger.Info().
		Str("subject", subject).
		Msg("NATS pub")
}

func (g *Gateway) subscribeBroadcast() {
	ctx := context.Background()
	flags := z21.Mask32(z21.SYSTEM_UPDATES)
	flags |= z21.Mask32(z21.CAN_DETECTOR_UPDATES)
	_, err := g.zc.SendRcv(ctx, &z21.BroadcastFlags{Flags: flags})
	if err != nil {
		g.logger.Error().
			Err(err)
	}
}

func (g *Gateway) natsCommandsLoop() error {
	subject := fmt.Sprintf("z21.%s.cmd.can.discover", g.name)
	g.logger.Info().
		Str("subject", subject).
		Msg("NATS sub")
	_, err := g.nc.Subscribe(subject, func(m *nats.Msg) {
		go g.handleCmdMessage(m)
	})
	if err != nil {
		return err
	}

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
			Err(err).
			Msg("NATS msg")
	}

	g.logger.Info().
		Str("subject", subject).
		Msg("NATS pub")
}

func (g *Gateway) doCmdRequest(msg *nats.Msg) CmdReply {
	g.logger.Debug().
		Str("subject", msg.Subject).
		Msg("NATS msg")
	switch msg.Subject {
	case fmt.Sprintf("z21.%s.cmd.can.discover", g.name):
		req := &z21.CanDetector{}
		fmt.Printf("%s\n", msg.Data)
		if err := json.Unmarshal(msg.Data, req); err != nil {
			return g.handleError(err)
		}
		return g.handleRequest(req)
	default:
		g.logger.Warn().
			Str("subject", msg.Subject).
			Msg("unknown subject")
		return CmdReply{}
	}
}

func (g *Gateway) handleError(err error) CmdReply {
	g.logger.Error().
		Err(err).
		Msg("NATS msg")
	return CmdReply{
		Ok:    false,
		Error: fmt.Sprintf("invalid message: %s", err),
		TS:    time.Now().Format(time.RFC3339),
	}
}

func (g *Gateway) handleRequest(req z21.Serializable) CmdReply {
	g.logger.Debug().Msgf("Z21 tx")

	ctx, cancel := context.WithTimeout(g.ctx, RequestTimeout)
	defer cancel()

	resp, err := g.zc.SendRcv(ctx, req)
	reply := CmdReply{
		TS: time.Now().Format(time.RFC3339),
	}
	if err != nil {
		g.logger.Error().
			Err(err).
			Msg("Z21 rx")
		reply.Ok = false
		reply.Error = fmt.Sprintf("%s", err)
	} else {
		g.logger.Debug().Msg("Z21 rx")
		reply.Ok = true
		reply.Data = resp
	}
	return reply
}
