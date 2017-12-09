package main

import (
	"fmt"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/nlopes/slack"
)

var reIP = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)
var reHostname = regexp.MustCompile(`(?m)(?:^| )((?:(?:[a-zA-Z]{1})|(?:[a-zA-Z]{1}[a-zA-Z]{1})|(?:[a-zA-Z]{1}[0-9]{1})|(?:[0-9]{1}[a-zA-Z]{1})|(?:[a-zA-Z0-9][a-zA-Z0-9-_.]{1,61}[a-zA-Z0-9]))\.(?:[a-zA-Z]{2,6}|[a-zA-Z0-9-]{2,30}\.[a-zA-Z]{2,3}))(?: |$)`)
var reUnlink = regexp.MustCompile(`<http[^\|]+\|([^>]+)>`)

func msgHandler(ev interface{}, msg *slack.Message, remove bool, botID string, reaction string) {
	if msg.User == botID || msg.Text == "" {
		logger.Printf("ignoring %s:%s: from bot or empty text", msg.Channel, msg.User)
		return
	}

	defer catchPanic(msg)

	var reactionUser string
	if reaction != "" {
		if radded, ok := ev.(*slack.ReactionAddedEvent); ok {
			reactionUser = radded.User
		} else if rdel, ok := ev.(*slack.ReactionRemovedEvent); ok {
			reactionUser = rdel.User
		}

		if reactionUser == "" {
			logger.Printf("skipping add/remove of reaction %s: user not found", reaction)
			return
		}

		if ok, _ := hostGroup.Exists(msg.Timestamp); ok {
			if remove {
				hostGroup.EditHighlight(msg.Timestamp, reactionUser, false)
				return
			}

			hostGroup.EditHighlight(msg.Timestamp, reactionUser, true)
			return
		} else if remove {
			// If the reaction was removed from a message which we weren't
			// tracking, don't do anything about it.
			return
		}
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
			addrs, err := net.LookupIP(hosts[i][1])
			if err != nil {
				continue
			}

			if ok, buffer := hostGroup.Exists(addrs[0].String()); ok {
				// Convert the reaction into a message, essentially, allowing
				// us to respond directly to them.
				if radded, ok := ev.(*slack.ReactionAddedEvent); ok {
					msg = slackRefToMessage(radded.Item.Channel, radded.User, radded.Item.Timestamp)
				} else if rdel, ok := ev.(*slack.ReactionRemovedEvent); ok {
					msg = slackRefToMessage(rdel.Item.Channel, rdel.User, rdel.Item.Timestamp)
				}
				slackReply(msg, true, fmt.Sprintf("<@%s>: %s already monitored, ignoring (%s)", msg.User, addrs[0].String(), buffer))
				continue
			}

			host := &Host{
				closer:         make(chan struct{}, 1),
				Origin:         msg,
				IP:             addrs[0],
				Added:          time.Now(),
				Buffer:         channelName,
				OriginReaction: reaction,
				Highlight:      []string{},
			}

			if reaction != "" {
				host.Buffer = "via reaction in " + host.Buffer
			}

			go host.Watch()
			hostGroup.Add(hosts[i][1], host)
		}

		return
	}

	// We should loop through each IP we find and track it.
	for _, ip := range ips {
		netIP := net.ParseIP(ip)

		// Make sure it's a valid ip, and also make sure that
		// we're not already tracking the ip.
		if netIP == nil {
			continue
		}
		if ok, buffer := hostGroup.Exists(netIP.String()); ok {
			if reaction != "" {
				// Convert the reaction into a message, essentially, allowing
				// us to respond directly to them.
				if radded, ok := ev.(*slack.ReactionAddedEvent); ok {
					msg = slackRefToMessage(radded.Item.Channel, radded.User, radded.Item.Timestamp)
				} else if rdel, ok := ev.(*slack.ReactionRemovedEvent); ok {
					msg = slackRefToMessage(rdel.Item.Channel, rdel.User, rdel.Item.Timestamp)
				}
				slackReply(msg, true, fmt.Sprintf("<@%s>: %s already monitored, ignoring (%s)", msg.User, netIP.String(), buffer))
			}
			continue
		}

		host := &Host{
			closer:         make(chan struct{}, 1),
			Origin:         msg,
			IP:             netIP,
			Added:          time.Now(),
			Buffer:         channelName,
			OriginReaction: reaction,
			Highlight:      []string{},
		}

		if reaction != "" {
			host.Buffer = "via reaction in " + host.Buffer
		}

		go host.Watch()
		hostGroup.Add(netIP.String(), host)
	}
}
