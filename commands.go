package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

var reCommand = regexp.MustCompile(`^!([[:word:]]+)(?: (.*)?)?`)

func cmdHandler(msg *slack.Message, cmd, args string) error {
	var reply string
	var err error

	switch cmd {
	case "enable":
		s := GetUserSettings(msg.User)
		if s.ChecksDisabled {
			s.ChecksDisabled = false
			SetUserSettings(s)
			reply = "*re-enabled automatic host checks for you.*"
			break
		}

		reply = "*automatic host checks already enabled for you.*"
		break
	case "disable":
		s := GetUserSettings(msg.User)
		if s.ChecksDisabled {
			reply = "*automatic host checks already disabled for you.*"
			break
		}

		s.ChecksDisabled = true
		SetUserSettings(s)
		reply = "*disabled automatic host checks for you, and flushing existing checks.*"

		hostGroup.GlobRemove("", msg.User)
		break
	case "active", "list", "listall", "all":
		dump := hostGroup.Dump()

		if dump == "" {
			reply = "no active hosts being monitored."
			break
		}

		reply = "```\n" + dump + "```"
		break
	case "clearall", "stopall", "killall":
		hostGroup.GlobRemove("", "")

		reply = "sending cancellation signal to active checks."
		break
	case "clear", "stop", "kill":
		if args == "" {
			hostGroup.GlobRemove("", msg.User)

			reply = "sending cancellation signal to *your* active checks."
			break
		}

		argv := strings.Fields(args)
		for _, query := range argv {
			hostGroup.GlobRemove(query, "")
		}

		reply = "sending cancellation signal to checks matching: `" + strings.Join(argv, "`, `") + "`"
		break
	case "ping", "check", "pong":
		argv := strings.Fields(args)

		if len(argv) == 0 {
			reply = "no hostname or ip address suppled."
			break
		}

		for _, query := range argv {
			var ip net.IP
			var addrs []net.IP

			ip = net.ParseIP(query)
			if ip == nil {
				addrs, err = net.LookupIP(query)
				if err != nil {
					reply += fmt.Sprintf("invalid addr/host: `%s`\n", query)
					continue
				}

				ip = addrs[0]
			}

			if ok, buffer := hostGroup.Exists(query); ok {
				reply = fmt.Sprintf("That host is already being monitored! (`%s`)", buffer)
				break
			}

			host := &Host{
				closer:    make(chan struct{}, 1),
				Origin:    msg,
				IP:        ip,
				Added:     time.Now(),
				Buffer:    "via !check",
				Highlight: []string{},
			}

			if ch := slackIDToChannel(msg.Channel); ch != "" {
				host.Buffer += " in " + ch
			}

			go host.Watch()
			err = hostGroup.Add(query, host)
			if !conf.NotifyOnStart {
				if err != nil {
					reply += fmt.Sprintf("error adding `%s`: %s\n", query, err)
					continue
				}

				reply += fmt.Sprintf("added check for `%s`\n", query)
			}
		}

		break
	case "help", "halp":
		reply = strings.Replace(`how2basic:
|!disable| disables *ponger* auto-monitoring (for you) and clears all of *your* checks
|!enable| enables *ponger* auto-monitoring (for you)
|!active| lists all active host/ip checks
|!clearall| clears all checks
|!clear [query]| clear checks matching *query*, or all of *your* checks
|!help| this help info
|message-reactions| start monitoring by adding the :%s: reaction to a message with an ip/host`, "|", "`", -1)
		reply = fmt.Sprintf(reply, conf.ReactionTrigger)
	default:
		reply = fmt.Sprintf("unknown command `%s`. use `!help`?", cmd)
	}

	if reply != "" {
		slackReply(msg, false, reply)
	}

	return nil
}
