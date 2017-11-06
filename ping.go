package main

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/paulstuart/ping"

	"github.com/nlopes/slack"
)

var hostGroup = Hosts{inv: make(map[string]*Host)}

type Hosts struct {
	sync.Mutex
	inv map[string]*Host
}

func (h *Hosts) Exists(id string) (ok bool) {
	h.Lock()
	defer h.Unlock()

	_, ok = h.inv[id]

	return ok
}

func (h *Hosts) Add(host *Host) error {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.inv[host.IP.String()]; ok {
		return errors.New("host already exists")
	}

	h.inv[host.IP.String()] = host
	return nil
}

func (h *Hosts) Remove(host *Host) {
	h.Lock()
	defer h.Unlock()

	delete(h.inv, host.IP.String())
}

type Host struct {
	closer chan struct{}
	Origin *slack.MessageEvent
	IP     net.IP
	Added  time.Time

	mu            sync.Mutex
	Online        bool
	LastOnline    time.Time
	LastOffline   time.Time
	TotalDowntime time.Duration
}

func (h *Host) Stop() {
	close(h.closer)
	hostGroup.Remove(h)
}

func (h *Host) Send(text string) {
	api := newSlackClient()
	_, _, _, err := api.SendMessage(
		h.Origin.Channel,
		slack.MsgOptionAsUser(true),
		slack.MsgOptionText(text, false),
	)
	if err != nil {
		logger.Printf("[%s::%s] error while attempting to send message to channel: %s", h.IP.String(), h.Origin.Msg.Username, err)
	}
}

func (h *Host) Sendf(format string, v ...interface{}) {
	h.Send(fmt.Sprintf(format, v...))
}

func (h *Host) Watch() {
	first := ping.Pinger(h.IP.String(), 2)
	if first == nil {
		h.Online = true
		h.LastOnline = time.Now()
		h.Sendf("host %s is online :ok_hand:", h.IP.String())
	} else {
		h.Online = false
		h.LastOffline = time.Now()
		h.Sendf("host %s is offline :radioactive_sign:", h.IP.String())
	}

	for {
		select {
		case <-h.closer:
			return
		default:
			time.Sleep(20 * time.Second)

			check := verifyHost(h.IP.String(), 3)
			if check == nil {
				if h.Online {
					// Host is still online.
					// TODO: check for surpased for removal?
				} else {
					// Host has become online.
					h.Online = true

					// Add up the downtime.
					h.TotalDowntime += time.Since(h.LastOffline)

					h.Sendf("<@%s> host %s now online (downtime counter: `%s`)", h.Origin.User, h.IP.String(), h.TotalDowntime)
				}

				h.LastOnline = time.Now()
				continue
			}

			// Assume host offline past this point.

			if h.Online {
				// Host was previously online, and is now offline.
				h.Online = false

				h.Sendf("<@%s> host %s now offline", h.Origin.User, h.IP.String())
			} else {
				// Host is still offline.
				h.TotalDowntime += time.Since(h.LastOffline)
			}

			h.LastOffline = time.Now()
		}
	}
}

func verifyHost(addr string, count int) (err error) {
	var previousError error
	for i := 0; i < count; i++ {
		previousError = err

		logger.Printf("pinging %s", addr)
		err = ping.Pinger(addr, 2)
		if previousError == nil && err != nil {
			return err
		}

		time.Sleep(10 * time.Second)
	}

	return err
}
