package main

import (
	"errors"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/paulstuart/ping"
	glob "github.com/ryanuber/go-glob"

	"github.com/nlopes/slack"
)

var hostGroup = Hosts{inv: make(map[string]*Host)}

type Hosts struct {
	sync.Mutex
	inv map[string]*Host
}

func (h *Hosts) Dump() (out string) {
	h.Lock()
	defer h.Unlock()

	var keys []string
	var maxLen int
	for key := range h.inv {
		if len(key) > maxLen {
			maxLen = len(key)
		}

		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, key := range keys {
		out += fmt.Sprintf(
			"q: %-"+strconv.Itoa(maxLen)+"s | ip: %-15s | watching: %8s | online: %t\n",
			key, h.inv[key].IP, time.Since(h.inv[key].Added).Truncate(time.Second), h.inv[key].Online,
		)
	}

	return out
}

func (h *Hosts) Clear(query, user string) {
	h.Lock()
	defer h.Unlock()

	for key := range h.inv {
		if query != "" {
			if glob.Glob(strings.ToLower(query), strings.ToLower(key)) {
				close(h.inv[key].closer)
			}
			return
		}

		if user != "" {
			if h.inv[key].Origin.User == user {
				close(h.inv[key].closer)
			}
			continue
		}

		close(h.inv[key].closer)
	}
}

func (h *Hosts) Exists(id string) (ok bool) {
	h.Lock()
	defer h.Unlock()

	_, ok = h.inv[id]

	return ok
}

func (h *Hosts) Add(id string, host *Host) error {
	h.Lock()
	defer h.Unlock()

	if _, ok := h.inv[id]; ok {
		return errors.New("host already tracked")
	}

	logger.Printf("added: %s", host.IP)

	h.inv[id] = host
	return nil
}

func (h *Hosts) Remove(host *Host) {
	h.Lock()
	defer h.Unlock()

	logger.Printf("removing: %s", host.IP)
	delete(h.inv, host.IP.String())
}

type Host struct {
	closer chan struct{}
	Origin *slack.MessageEvent
	IP     net.IP
	Added  time.Time

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
	outChannel, err := lookupChannel(conf.OutgoingChannel)
	if err != nil {
		logger.Printf("error checking %s: %s", h.IP, err)
		return
	}

	api := newSlackClient()
	// _, _, _, err = api.SendMessage(
	// 	outChannel,
	// 	slack.MsgOptionAsUser(true),
	// 	slack.MsgOptionText(text, false),
	// )
	params := slack.NewPostMessageParameters()
	params.AsUser = true
	params.Text = text
	// Don't use this if you don't want threads.
	params.ThreadTimestamp = h.Origin.Timestamp

	_, _, err = api.PostMessage(outChannel, text, params)

	if err != nil {
		logger.Printf("[%s::%s] error while attempting to send message to channel: %s", h.IP, h.Origin.Msg.Username, err)
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
		if conf.NotifyOnStart {
			h.Sendf("*%s* online :white_check_mark:", h.IP.String())
		}
	} else {
		h.Online = false
		h.LastOffline = time.Now()
		if conf.NotifyOnStart {
			h.Sendf("*%s* offline :warn1:", h.IP.String())
		}
	}

	for {
		select {
		case <-h.closer:
			hostGroup.Remove(h)
			return
		case <-time.After(10 * time.Second):
			if time.Since(h.Added) > time.Duration(conf.ForcedTimeout)*time.Second {
				logger.Printf("removing %s: total time exceeds %d seconds", h.IP, conf.ForcedTimeout)
				hostGroup.Remove(h)
				return
			}

			var check error
			for i := 0; i < 3; i++ {
				select {
				case <-h.closer:
					hostGroup.Remove(h)
					return
				case <-time.After(10 * time.Second):
				}

				logger.Printf("pinging %s [%d/3]", h.IP.String(), i+1)
				check = ping.Pinger(h.IP.String(), 2)
				if check != nil {
					break
				}
			}

			if check == nil {
				if h.Online {
					// Host is still online.
				} else {
					// Host has become online.
					h.Online = true

					// Add up the downtime.
					h.TotalDowntime += time.Since(h.LastOffline)

					h.Sendf("*%s* now online (downtime: `%s`) :white_check_mark:", h.IP, h.TotalDowntime.Truncate(time.Minute))
				}

				h.LastOnline = time.Now()

				if (h.LastOffline.IsZero() && time.Since(h.Added) > time.Duration(conf.RemovalTimeout)*time.Second) ||
					(!h.LastOffline.IsZero() && time.Since(h.LastOffline) > time.Duration(conf.RemovalTimeout)*time.Second) {
					logger.Printf("removing %s: time since last down > %s", h.IP, time.Duration(conf.RemovalTimeout)*time.Second)
					hostGroup.Remove(h)
					return
				}
				continue
			}

			// Assume host offline past this point.

			if h.Online {
				// Host was previously online, and is now offline.
				h.Online = false

				h.Sendf("*%s* now offline :warn1:", h.IP)
			} else {
				// Host is still offline.
				h.TotalDowntime += time.Since(h.LastOffline)
			}

			h.LastOffline = time.Now()
		}
	}
}
