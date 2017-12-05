package main

import (
	"errors"
	"strings"
	"sync"

	"github.com/nlopes/slack"
)

func newSlackClient() *slack.Client {
	api := slack.New(conf.Token)
	api.SetDebug(flags.Debug)

	return api
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
	params.ThreadTimestamp = msg.ThreadTimestamp
	params.EscapeText = false
	_, _, err := api.PostMessage(msg.Channel, text, params)

	if err != nil {
		logger.Printf("error replying to %s:%s: %s", msg.Channel, msg.User, err)
	}
}
