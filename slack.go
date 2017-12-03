package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/nlopes/slack"
)

func newSlackClient() *slack.Client {
	api := slack.New(conf.Token)
	api.SetDebug(flags.Debug)

	return api
}

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
				// rtm.SendMessage(rtm.NewOutgoingMessage("_bot has been restarted_", channelID))
			case *slack.MessageEvent, *slack.ReactionAddedEvent, *slack.ReactionRemovedEvent:
				msgHandler(msg.Data, botID)
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

func msgHandler(data interface{}, botID string) {
	ev := &Event{src: data}

	// TODO: reaction is going to be blank?
	if ev.User() == botID {
		return
	}

	if ev.Text() == "" && ev.IsMessage() {
		return
	}

	ev.Buffer = slackIDToChannel(ev.Channel())

	logger.Println(ev.String())

	if strings.ToLower(ev.Buffer) != strings.ToLower(conf.IncomingChannel) && ev.Buffer != "" {
		logger.Printf("skipping: %q not input channel or PM", ev.Buffer)
		return
	}

	// TODO: check if reaction, and if so, go through history to look for the
	// recent event matching it.
	cmd := reCommand.FindStringSubmatch(ev.Text())
	if len(cmd) == 3 && cmd[1] != "" {
		err := cmdHandler(ev, cmd[1], cmd[2])
		if err != nil {
			logger.Printf("unable to execute command handler for %q %q: %s", cmd[1], cmd[2], err)
		}
		return
	}

	// Check if they want automagical checks.
	set := GetUserSettings(ev.User())
	if set.ChecksDisabled {
		return
	}

	ips := reIP.FindAllString(ev.Text(), -1)
	if len(ips) == 0 {
		// Check for hostnames.
		hosts := reHostname.FindAllStringSubmatch(ev.Text(), -1)

		if hosts == nil {
			return
		}

		for i := 0; i < len(hosts); i++ {
			addrs, err := net.LookupIP(hosts[i][1])
			if err != nil || hostGroup.Exists(addrs[0].String()) {
				continue
			}

			host := &Host{
				closer: make(chan struct{}, 1),
				Origin: ev,
				IP:     addrs[0],
				Added:  time.Now(),
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
		hostGroup.Add(netIP.String(), host)

		if conf.ReactionOnStart {
			host.AddReaction("white_check_mark")
		}
	}
}

func cmdHandler(ev *Event, cmd, args string) error {
	cmd = strings.ToLower(cmd)
	api := newSlackClient()
	var err error

	params := slack.NewPostMessageParameters()
	params.AsUser = true
	params.ThreadTimestamp = ev.ThreadTimestamp()
	var reply string

	switch cmd {
	case "enable":
		s := GetUserSettings(ev.User())
		if s.ChecksDisabled {
			s.ChecksDisabled = false
			SetUserSettings(s)
			reply = "*re-enabled automatic host checks for you.*"
			break
		}

		reply = "*automatic host checks already enabled for you.*"
		break
	case "disable":
		s := GetUserSettings(ev.User())
		if s.ChecksDisabled {
			reply = "*automatic host checks already disabled for you.*"
			break
		}

		s.ChecksDisabled = true
		SetUserSettings(s)
		reply = "*disabled automatic host checks for you, and flushing existing checks.*"

		hostGroup.Clear("", ev.User())
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
		hostGroup.Clear("", "")

		reply = "sending cancellation signal to active checks."
		break
	case "clear", "stop", "kill":
		if args == "" {
			hostGroup.Clear("", ev.User())

			reply = "sending cancellation signal to *your* active checks."
			break
		}

		argv := strings.Fields(args)
		for _, query := range argv {
			hostGroup.Clear(query, "")
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

			ev.Buffer = "via !check"

			host := &Host{
				closer: make(chan struct{}, 1),
				Origin: ev,
				IP:     ip,
				Added:  time.Now(),
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
|!help| this help info :doge:`, "|", "`", -1)
	default:
		reply = fmt.Sprintf("unknown command `%s`. use `!help`?", cmd)
	}

	if reply != "" {
		_, _, err = api.PostMessage(ev.Channel(), reply, params)
		return err
	}

	return nil
}

var channelCache = struct {
	sync.Mutex
	cache map[string]string
}{cache: make(map[string]string)}

func lookupChannel(name string) (string, error) {
	channelCache.Lock()
	defer channelCache.Unlock()

	id, ok := channelCache.cache[name]
	if ok {
		return id, nil
	}

	var err error
	if id, err = slackChannelID(name); err != nil {
		return "", err
	}

	channelCache.cache[name] = id

	return id, nil
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

func slackIDToChannel(cid string) string {
	channelCache.Lock()
	defer channelCache.Unlock()

	id, ok := channelCache.cache[cid]
	if ok {
		return id
	}

	api := newSlackClient()
	// Regular channel.
	if ch, err := api.GetChannelInfo(cid); err == nil {
		channelCache.cache[cid] = "#" + ch.Name
	} else if ch, err := api.GetGroupInfo(cid); err == nil {
		// Private channel.
		channelCache.cache[cid] = "#" + ch.Name
	} else {
		// PM or group message?
		channelCache.cache[cid] = ""
	}

	return channelCache.cache[cid]
}
