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
				delete(h.inv, key)
			}
			if glob.Glob(query, h.inv[key].IP.String()) {
				close(h.inv[key].closer)
				delete(h.inv, key)
			}
			return
		}

		if user != "" {
			if h.inv[key].Origin.User == user {
				close(h.inv[key].closer)
				delete(h.inv, key)
			}
			continue
		}

		close(h.inv[key].closer)
	}
}

func (h *Hosts) Exists(id string) (ok bool) {
	h.Lock()
	defer h.Unlock()

	id = strings.ToLower(id)

	for key := range h.inv {
		if key == id {
			return true
		}

		if h.inv[key].IP.String() == id {
			return true
		}
	}

	return false
}

func (h *Hosts) Add(id string, host *Host) error {
	h.Lock()
	defer h.Unlock()

	id = strings.ToLower(id)

	if _, ok := h.inv[id]; ok {
		return errors.New("host already tracked")
	}

	logger.Printf("added: %s", host.IP)

	h.inv[id] = host
	return nil
}

func (h *Hosts) Remove(host *Host, reason string) {
	h.Lock()
	defer h.Unlock()

	logger.Printf("removing: %s: %s", host.IP, reason)
	for key := range h.inv {
		if h.inv[key].IP.Equal(host.IP) {
			if !h.inv[key].HasSentFirstReply {
				h.inv[key].Send(reason)
				h.inv[key].RemoveReaction("white_check_mark")
			}

			delete(h.inv, key)
		}
	}
}

type Host struct {
	closer            chan struct{}
	Origin            *slack.MessageEvent
	Source            string
	IP                net.IP
	Added             time.Time
	HasSentFirstReply bool

	Online        bool
	LastOnline    time.Time
	LastOffline   time.Time
	TotalDowntime time.Duration
}

func (h *Host) AddReaction(action string) {
	api := newSlackClient()
	if err := api.AddReaction(action, slack.NewRefToMessage(h.Origin.Channel, h.Origin.Timestamp)); err != nil {
		logger.Printf("error adding reaction %q to %q: %q", action, h.Origin.Channel, err)
	}
}

func (h *Host) RemoveReaction(action string) {
	api := newSlackClient()
	if err := api.RemoveReaction(action, slack.NewRefToMessage(h.Origin.Channel, h.Origin.Timestamp)); err != nil {
		logger.Printf("error removing reaction %q to %q: %q", action, h.Origin.Channel, err)
	}
}

func (h *Host) Send(text string) {
	// outChannel, err := lookupChannel(conf.OutgoingChannel)
	// if err != nil {
	// 	logger.Printf("error checking %s: %s", h.IP, err)
	// 	return
	// }

	api := newSlackClient()

	params := slack.NewPostMessageParameters()
	params.AsUser = true

	// Don't use this if you don't want threads.
	params.ThreadTimestamp = h.Origin.Timestamp

	_, _, err := api.PostMessage(h.Origin.Channel, text, params)

	if err != nil {
		logger.Printf("[%s::%s] error while attempting to send message to channel: %s", h.IP, h.Origin.Msg.Username, err)
	}
}

func (h *Host) Sendf(format string, v ...interface{}) {
	if !h.HasSentFirstReply && conf.ReactionOnStart {
		// Remove the "check" reaction we added at the start.
		h.RemoveReaction("white_check_mark")
	}

	// If we've not sent the first reply and if we're not notifying on start.
	// If we are notifying on start, then make sure this is the 'first' message
	// by checking the LasstOnline/LastOffline which are only updated after
	// the first message is sent.
	if !h.HasSentFirstReply && (!conf.NotifyOnStart || conf.NotifyOnStart && h.LastOnline.IsZero() && h.LastOffline.IsZero()) {
		h.HasSentFirstReply = true
	}

	h.Send(fmt.Sprintf(format, v...))
}

func (h *Host) Watch() {
	first := ping.Pinger(h.IP.String(), 2)
	if first == nil {
		if conf.NotifyOnStart {
			h.Sendf("*%s* online :white_check_mark:", h.IP.String())
		}
		h.Online = true
		h.LastOnline = time.Now()
	} else {
		if conf.NotifyOnStart {
			h.Sendf("*%s* offline :warn1:", h.IP.String())
		}
		h.Online = false
		h.LastOffline = time.Now()
	}

	for {
		select {
		case <-h.closer:
			hostGroup.Remove(h, fmt.Sprintf("checks for *%s* have been cleared upon request", h.IP))
			return
		case <-time.After(10 * time.Second):
			if time.Since(h.Added) > time.Duration(conf.ForcedTimeout)*time.Second {
				hostGroup.Remove(h, fmt.Sprintf("stopped monitoring *%s*: checks exceeded `%s`", h.IP, time.Duration(conf.ForcedTimeout)*time.Second))
				return
			}

			var check error
			for i := 0; i < 3; i++ {
				select {
				case <-h.closer:
					hostGroup.Remove(h, fmt.Sprintf("checks for *%s* have been cleared upon request", h.IP))
					return
				case <-time.After(2 * time.Second):
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

					h.Sendf("*%s* now online (downtime: `%s`) :white_check_mark:", h.IP, h.TotalDowntime.Truncate(time.Second))
				}

				h.LastOnline = time.Now()

				if (h.LastOffline.IsZero() && time.Since(h.Added) > time.Duration(conf.RemovalTimeout)*time.Second) ||
					(!h.LastOffline.IsZero() && time.Since(h.LastOffline) > time.Duration(conf.RemovalTimeout)*time.Second) {
					hostGroup.Remove(h, fmt.Sprintf("stopped monitoring *%s*: time since last offline `>%s`", h.IP, time.Duration(conf.RemovalTimeout)*time.Second))
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
