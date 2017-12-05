package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

var reIP = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
var reHostname = regexp.MustCompile(`(?m)(?:^| )((?:(?:[a-zA-Z]{1})|(?:[a-zA-Z]{1}[a-zA-Z]{1})|(?:[a-zA-Z]{1}[0-9]{1})|(?:[0-9]{1}[a-zA-Z]{1})|(?:[a-zA-Z0-9][a-zA-Z0-9-_.]{1,61}[a-zA-Z0-9]))\.(?:[a-zA-Z]{2,6}|[a-zA-Z0-9-]{2,30}\.[a-zA-Z]{2,3}))(?: |$)`)
var reCommand = regexp.MustCompile(`^!([[:word:]]+)(?: (.*)?)?`)
var reUnlink = regexp.MustCompile(`<http[^\|]+\|([^>]+)>`)

func newSlackRTM(messageChan chan string) error {
	channelID, err := lookupChannel(conf.IncomingChannel)
	if err != nil {
		return err
	}

	var botID string

	api := newSlackClient()
	rtm := api.NewRTM()
	go rtm.ManageConnection()
	defer rtm.Disconnect()

	for {
		select {
		case msg := <-rtm.IncomingEvents:
			switch ev := msg.Data.(type) {
			case *slack.ConnectedEvent:
				logger.Printf(
					"connected to %s.slack.com: %s (%d users, %d channels, user %q)",
					ev.Info.Team.Domain,
					ev.Info.Team.Name,
					len(ev.Info.Users),
					len(ev.Info.Channels),
					ev.Info.User.Name,
				)
				botID = ev.Info.User.ID
				rtm.SendMessage(rtm.NewOutgoingMessage("_bot has been restarted (all checks flushed)_", channelID))
			case *slack.MessageEvent:
				msgHandler(msg.Data, &slack.Message{Msg: ev.Msg, SubMessage: ev.SubMessage}, false, botID, "")
			case *slack.ReactionAddedEvent:
				if ev.Reaction != conf.ReactionTrigger {
					break
				}

				hist := slackMsgFromReaction(ev.Item.Channel, ev.Item.Timestamp)
				if hist == nil || len(hist.Messages) == 0 {
					break
				}

				msgHandler(msg.Data, &hist.Messages[0], false, botID, ev.Reaction)
			case *slack.ReactionRemovedEvent:
				if ev.Reaction != conf.ReactionTrigger {
					break
				}

				hist := slackMsgFromReaction(ev.Item.Channel, ev.Item.Timestamp)
				if hist == nil || len(hist.Messages) == 0 {
					break
				}

				msgHandler(msg.Data, &hist.Messages[0], true, botID, ev.Reaction)
			case *slack.RTMError:
				return ev
			case *slack.InvalidAuthEvent:
				return errors.New("invalid credentials")
			case *slack.DisconnectedEvent:
				if ev.Intentional {
					return nil
				}
			default:
			}
		case msg := <-messageChan:
			rtm.SendMessage(rtm.NewOutgoingMessage(msg, channelID))
		}
	}

	return nil
}

func msgHandler(ev interface{}, msg *slack.Message, remove bool, botID string, reaction string) {
	if msg.User == botID || msg.Text == "" {
		return
	}

	channelName := slackIDToChannel(msg.Channel)

	if _, ok := ev.(*slack.MessageEvent); ok {
		logger.Printf("<%s[%s]:%s> %s", msg.Channel, channelName, msg.User, msg.Text)
	}

	msg.Text = reUnlink.ReplaceAllString(msg.Text, "$1")

	cmd := reCommand.FindStringSubmatch(msg.Text)
	// Only parse it as a command, if it's not a reaction based message.
	if reaction == "" && len(cmd) == 3 && cmd[1] != "" {
		cmd[1] = strings.ToLower(cmd[1])
		// Allow some commands even if it's not in the incoming channel.
		if strings.ToLower(channelName) != strings.ToLower(conf.IncomingChannel) && channelName != "" {
			if cmd[1] == "help" || cmd[1] == "halp" {
				return
			}
		}

		err := cmdHandler(msg, cmd[1], cmd[2])
		if err != nil {
			logger.Printf("unable to execute command handler for %q %q: %s", cmd[1], cmd[2], err)
		}
		return
	}

	if reaction == "" && strings.ToLower(channelName) != strings.ToLower(conf.IncomingChannel) && channelName != "" {
		logger.Printf("skipping: %q not input channel or PM, and not reaction", channelName)
		return
	}

	// Check if they want automagical checks.
	set := GetUserSettings(msg.User)
	if set.ChecksDisabled {
		return
	}

	ips := reIP.FindAllString(msg.Text, -1)
	if len(ips) == 0 {
		// Check for hostnames.
		hosts := reHostname.FindAllStringSubmatch(msg.Text, -1)

		if hosts == nil {
			return
		}

		for i := 0; i < len(hosts); i++ {
			if remove {
				if hostGroup.LRemove(hosts[i][1], "") {
					slackReply(msg, "no longer monitoring: "+hosts[i][1])
				}
				continue
			}

			addrs, err := net.LookupIP(hosts[i][1])
			if err != nil || hostGroup.Exists(addrs[0].String()) {
				continue
			}

			host := &Host{
				closer:         make(chan struct{}, 1),
				Origin:         msg,
				IP:             addrs[0],
				Added:          time.Now(),
				Buffer:         channelName,
				OriginReaction: reaction,
			}

			go host.Watch()
			hostGroup.Add(hosts[i][1], host)

			if conf.ReactionOnStart {
				host.AddReaction("white_check_mark")
			}
		}

		return
	}

	// We should loop through each IP we find and track it.
	for _, ip := range ips {
		netIP := net.ParseIP(ip)

		if remove && netIP != nil {
			if hostGroup.LRemove(netIP.String(), "") {
				slackReply(msg, "no longer monitoring: "+netIP.String())
			}
			continue
		}

		// Make sure it's a valid ip, and also make sure that
		// we're not already tracking the ip.
		if netIP == nil || hostGroup.Exists(netIP.String()) {
			continue
		}

		host := &Host{
			closer:         make(chan struct{}, 1),
			Origin:         msg,
			IP:             netIP,
			Added:          time.Now(),
			Buffer:         channelName,
			OriginReaction: reaction,
		}

		go host.Watch()
		hostGroup.Add(netIP.String(), host)

		if conf.ReactionOnStart {
			host.AddReaction("white_check_mark")
		}
	}
}

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

			host := &Host{
				closer: make(chan struct{}, 1),
				Origin: msg,
				IP:     ip,
				Added:  time.Now(),
				Buffer: "via !check",
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
|!disable| disables *ponger* auto-monitoring and clears all of *your* checks
|!enable| enables *ponger* auto-monitoring
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
		slackReply(msg, reply)
	}

	return nil
}
