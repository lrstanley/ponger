package main

import (
	"errors"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/nlopes/slack"
	"github.com/y0ssar1an/q"
)

func newSlackClient() *slack.Client {
	api := slack.New(conf.Token)
	api.SetDebug(flags.Debug)

	return api
}

func slackChannelID(channelName string) (string, error) {
	if strings.HasPrefix(channelName, "#") {
		channelName = strings.Replace(channelName, "#", "", 1)
	}
	channelName = strings.ToLower(channelName)

	api := newSlackClient()

	// Public channels.
	channels, err := api.GetChannels(true)
	if err != nil {
		return "", err
	}

	for i := 0; i < len(channels); i++ {
		if strings.ToLower(channels[i].Name) == channelName {
			return channels[i].ID, nil
		}
	}

	// I.e. "private" channels.
	groups, err := api.GetGroups(true)
	if err != nil {
		return "", err
	}

	for i := 0; i < len(groups); i++ {
		if strings.ToLower(groups[i].Name) == channelName {
			return groups[i].ID, nil
		}
	}

	return "", errors.New("channel not found")
}

var reIP = regexp.MustCompile(`\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`)

func newSlack(messageChan chan string) error {
	channelID, err := slackChannelID(conf.Channel)
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
				botID = ev.Info.User.ID
				rtm.SendMessage(rtm.NewOutgoingMessage("_started up_", channelID))
			case *slack.MessageEvent:
				if ev.User == botID {
					continue
				}

				logger.Printf("<%s:%s> %s", ev.Channel, ev.User, ev.Text)

				ips := reIP.FindAllString(ev.Text, -1)
				if len(ips) == 0 {
					continue
				}

				q.Q(ev)

				// We should loop through each IP we find and track it.
				for _, ip := range ips {
					netIP := net.ParseIP(ip)

					// Make sure it's a valid ip, and also make sure that
					// we're not already tracking the ip.
					if netIP == nil || hostGroup.Exists(netIP.String()) {
						continue
					}

					host := &Host{
						closer: make(chan struct{}, 1),
						Origin: ev,
						IP:     netIP,
						Added:  time.Now(),
					}

					go host.Watch()
					hostGroup.Add(host)

					err = rtm.AddReaction("white_check_mark", slack.NewRefToMessage(ev.Channel, ev.Timestamp))
					if err != nil && !strings.Contains(err.Error(), "already_reacted") {
						logger.Printf("error adding reaction: %s", err)
					}
				}
			case *slack.RTMError:
				return ev
			case *slack.InvalidAuthEvent:
				return errors.New("invalid credentials")
			case *slack.DisconnectedEvent:
				if ev.Intentional {
					return nil
				}
			default:
				// fmt.Printf("Unexpected: %v\n", msg.Data)
			}
		case msg := <-messageChan:
			rtm.SendMessage(rtm.NewOutgoingMessage(msg, channelID))
		}
	}

	return nil
}
