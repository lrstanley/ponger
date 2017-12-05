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

	"github.com/nlopes/slack"
	"github.com/paulstuart/ping"
	glob "github.com/ryanuber/go-glob"
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
			"q: %-"+strconv.Itoa(maxLen)+"s | ip: %-15s | watching: %8s | online: %-5t | src: %s\n",
			key, h.inv[key].IP, time.Since(h.inv[key].Added).Truncate(time.Second), h.inv[key].Online,
			h.inv[key].Buffer,
		)
	}

	return out
}

func (h *Hosts) GlobRemove(query, user string) {
	h.Lock()
	defer h.Unlock()

	for key := range h.inv {
		if query != "" {
			if glob.Glob(strings.ToLower(query), strings.ToLower(key)) {
				h.Remove(key, "checks cancelled")
				continue
			}

			if glob.Glob(query, h.inv[key].IP.String()) {
				h.Remove(key, "checks cancelled")
				continue
			}
			return
		}

		if user != "" {
			if h.inv[key].Origin.User == user {
				h.Remove(key, "checks cancelled")
			}
			continue
		}

		h.Remove(key, "check cancelled")
	}
}

func (h *Hosts) Exists(id string) (ok bool, buffer string) {
	h.Lock()
	defer h.Unlock()

	id = strings.ToLower(id)

	for key := range h.inv {
		if key == id {
			return true, h.inv[key].Buffer
		}

		if h.inv[key].IP.String() == id {
			return true, h.inv[key].Buffer
		}

		if h.inv[key].Origin.EventTimestamp == id {
			return true, h.inv[key].Buffer
		}
	}

	return false, ""
}

func (h *Hosts) Add(id string, host *Host) error {
	h.Lock()
	defer h.Unlock()

	id = strings.ToLower(id)

	host.ID = id

	if _, ok := h.inv[id]; ok {
		return errors.New("host already tracked")
	}

	logger.Printf("added: %s", host.IP)

	h.inv[id] = host
	return nil
}

func (h *Hosts) Remove(id, reason string) bool {
	id = strings.ToLower(id)

	if _, ok := h.inv[id]; ok {
		if !h.inv[id].HasSentFirstReply {
			h.inv[id].Send(reason)
		}

		close(h.inv[id].closer)
		delete(h.inv, id)
		return true
	}

	return false
}

func (h *Hosts) LRemove(id, reason string) bool {
	h.Lock()
	ok := h.Remove(id, reason)
	h.Unlock()

	return ok
}

func (h *Hosts) EditHighlight(ts, user string, add bool) {
	h.Lock()
	defer h.Unlock()

	for key := range h.inv {
		if h.inv[key].Origin.EventTimestamp != ts {
			continue
		}

		if add {
			if user == h.inv[key].Origin.User {
				continue
			}

			h.inv[key].Highlight = append(h.inv[key].Highlight, user)
		} else {
			hl := []string{}
			for _, uid := range h.inv[key].Highlight {
				if uid != user {
					hl = append(hl, uid)
				}
			}

			h.inv[key].Highlight = hl
		}

		if len(h.inv[key].Highlight) == 0 && h.inv[key].OriginReaction != "" {
			h.inv[key].Send("no longer monitoring: " + h.inv[key].IP.String())
			_ = h.Remove(h.inv[key].ID, "")
		}
	}
}

type Host struct {
	ID                string
	closer            chan struct{}
	Origin            *slack.Message
	OriginReaction    string
	Buffer            string
	IP                net.IP
	Added             time.Time
	HasSentFirstReply bool
	Highlight         []string

	Online        bool
	LastOnline    time.Time
	LastOffline   time.Time
	TotalDowntime time.Duration
}

func (h *Host) Send(text string) {
	// If we've not sent the first reply and if we're not notifying on start.
	// If we are notifying on start, then make sure this is the 'first' message
	// by checking the LasstOnline/LastOffline which are only updated after
	// the first message is sent.
	if !h.HasSentFirstReply && (!conf.NotifyOnStart || conf.NotifyOnStart && h.LastOnline.IsZero() && h.LastOffline.IsZero()) {
		h.HasSentFirstReply = true
	}

	if len(h.Highlight) > 0 {
		text = "<@" + strings.Join(h.Highlight, "> <@") + ">: " + text
	}

	slackReply(h.Origin, text)
}

func (h *Host) Sendf(format string, v ...interface{}) {
	h.Send(fmt.Sprintf(format, v...))
}

func (h *Host) Watch() {
	defer hostGroup.LRemove(h.ID, "")

	first := ping.Pinger(h.IP.String(), 2)
	if first == nil {
		if conf.NotifyOnStart {
			h.Sendf("%s online :white_check_mark:", h.IP.String())
		}
		h.Online = true
		h.LastOnline = time.Now()
	} else {
		if conf.NotifyOnStart {
			h.Sendf("%s offline :warn1:", h.IP.String())
		}
		h.Online = false
		h.LastOffline = time.Now()
	}

	for {
		select {
		case <-h.closer:
			return
		case <-time.After(5 * time.Second):
			if time.Since(h.Added) > time.Duration(conf.ForcedTimeout)*time.Second {
				hostGroup.LRemove(h.ID, fmt.Sprintf("stopped monitoring %s: checks exceeded `%s`", h.IP, time.Duration(conf.ForcedTimeout)*time.Second))
				return
			}

			var check error
			var bad int
			for i := 0; i < 3; i++ {
				select {
				case <-h.closer:
					return
				case <-time.After(2 * time.Second):
				}

				logger.Printf("pinging %s [%d/3]", h.IP.String(), i+1)
				check = ping.Pinger(h.IP.String(), 2)
				if check != nil {
					bad++
				}
			}

			if bad != 3 {
				check = nil
			}

			if check == nil {
				if h.Online {
					// Host is still online.
				} else {
					// Host has become online.
					h.Online = true

					// Add up the downtime.
					h.TotalDowntime += time.Since(h.LastOffline)

					h.Sendf("%s now online (downtime: `%s`) :white_check_mark:", h.IP, h.TotalDowntime.Truncate(time.Second))
				}

				h.LastOnline = time.Now()

				if (h.LastOffline.IsZero() && time.Since(h.Added) > time.Duration(conf.RemovalTimeout)*time.Second) ||
					(!h.LastOffline.IsZero() && time.Since(h.LastOffline) > time.Duration(conf.RemovalTimeout)*time.Second) {
					hostGroup.LRemove(h.ID, fmt.Sprintf("stopped monitoring %s: time since last offline `>%s`", h.IP, time.Duration(conf.RemovalTimeout)*time.Second))
					return
				}

				// Since it's healthy, wait a bit before trying to check if it's offline.
				// This should help prevent a bit of spam if the service is flapping.
				time.Sleep(25 * time.Second)
				continue
			}

			// Assume host offline past this point.

			if h.Online {
				// Host was previously online, and is now offline.
				h.Online = false

				h.Sendf("%s now offline :warn1:", h.IP)
			} else {
				// Host is still offline.
				h.TotalDowntime += time.Since(h.LastOffline)
			}

			h.LastOffline = time.Now()
		}
	}
}
