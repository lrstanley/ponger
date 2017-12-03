package main

import (
	"fmt"

	"github.com/nlopes/slack"
)

type Event struct {
	src    interface{}
	Buffer string
}

func (e *Event) IsMessage() bool {
	_, ok := e.src.(*slack.MessageEvent)
	return ok
}

func (e *Event) ReactionAdded() bool {
	switch e.src.(type) {
	case *slack.ReactionAddedEvent:
		return true
	case *slack.ReactionRemovedEvent:
		return false
	default:
		panic("unknown event type")
	}
}

func (e *Event) Channel() string {
	switch ev := e.src.(type) {
	case *slack.MessageEvent:
		return ev.Channel
	case *slack.ReactionAddedEvent:
		return ev.Item.Channel
	case *slack.ReactionRemovedEvent:
		return ev.Item.Channel
	default:
		panic("unknown event type")
	}
}

func (e *Event) User() string {
	switch ev := e.src.(type) {
	case *slack.MessageEvent:
		return ev.User
	case *slack.ReactionAddedEvent:
		return ev.ItemUser
	case *slack.ReactionRemovedEvent:
		return ev.ItemUser
	default:
		panic("unknown event type")
	}
}

func (e *Event) Timestamp() string {
	switch ev := e.src.(type) {
	case *slack.MessageEvent:
		return ev.Timestamp
	case *slack.ReactionAddedEvent:
		return ev.EventTimestamp
	case *slack.ReactionRemovedEvent:
		return ev.EventTimestamp
	default:
		panic("unknown event type")
	}
}

func (e *Event) ThreadTimestamp() string {
	switch ev := e.src.(type) {
	case *slack.MessageEvent:
		return ev.ThreadTimestamp
	case *slack.ReactionAddedEvent:
		return ev.Item.Timestamp
	case *slack.ReactionRemovedEvent:
		return ev.Item.Timestamp
	default:
		panic("unknown event type")
	}
}

func (e *Event) Text() string {
	switch ev := e.src.(type) {
	case *slack.MessageEvent:
		return reUnlink.ReplaceAllString(ev.Text, "$1")
	case *slack.ReactionAddedEvent:
		return ev.Reaction
	case *slack.ReactionRemovedEvent:
		return ev.Reaction
	default:
		panic("unknown event type")
	}
}

func (e *Event) String() string {
	switch e.src.(type) {
	case *slack.MessageEvent:
		return fmt.Sprintf("<%s[%s]:%s> %s", e.Channel(), e.Buffer, e.User(), e.Text())
	case *slack.ReactionAddedEvent:
		return fmt.Sprintf("<%s[%s]:%s> added reaction: %s", e.Channel(), e.Buffer, e.User(), e.Text())
	case *slack.ReactionRemovedEvent:
		return fmt.Sprintf("<%s[%s]:%s> removed reaction: %s", e.Channel(), e.Buffer, e.User(), e.Text())
	default:
		panic("unknown event type")
	}
}
