package main

import (
	"errors"
	"strings"
	"sync"

	"github.com/nlopes/slack"
	"github.com/y0ssar1an/q"
)

func newSlackClient() *slack.Client {
	api := slack.New(conf.Token)
	api.SetDebug(flags.Debug)

	return api
}

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

	firstConnection := true

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

				if firstConnection {
					rtm.SendMessage(rtm.NewOutgoingMessage("_bot has been restarted (all checks flushed)_", channelID))
					firstConnection = false
				}
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

func slackMsgFromReaction(channel string, ts string) (hist *slack.History) {
	api := newSlackClient()
	var err error

	params := slack.HistoryParameters{Oldest: ts, Inclusive: true}

	hist, err = api.GetChannelHistory(channel, params)
	if err != nil {
		hist, err = api.GetGroupHistory(channel, params)
	}
	if err != nil {
		hist, err = api.GetIMHistory(channel, params)
	}

	if err != nil {
		logger.Printf("cannot lookup history for %s:%s: %s", channel, ts, err)
		return nil
	}

	for i := 0; i < len(hist.Messages); i++ {
		hist.Messages[i].Channel = channel
	}

	return hist
}

func slackReply(msg *slack.Message, text string) {
	api := newSlackClient()

	params := slack.NewPostMessageParameters()
	params.AsUser = true

	q.Q(msg)

	params.ThreadTimestamp = msg.ThreadTimestamp
	if params.ThreadTimestamp == "" {
		params.ThreadTimestamp = msg.Timestamp
	}
	if params.ThreadTimestamp == "" {
		params.ThreadTimestamp = msg.EventTimestamp
	}

	params.EscapeText = false
	_, _, err := api.PostMessage(msg.Channel, text, params)

	if err != nil {
		logger.Printf("error replying to %s:%s: %s", msg.Channel, msg.User, err)
	}
}

func slackRefToMessage(channel, user, ts string) *slack.Message {
	return &slack.Message{Msg: slack.Msg{Channel: channel, Timestamp: ts, User: user}}
}
