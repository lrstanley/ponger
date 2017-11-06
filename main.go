package main

import (
	"fmt"
	"log"
	"os"

	"github.com/BurntSushi/toml"
	gflags "github.com/jessevdk/go-flags"
	"github.com/nlopes/slack"
)

// TODO:
//  - doger? must doge names.
//  - !registerpings?
//  - use threads to update the user?
//  - use reactions to let the user know it's tracking it?
//  - list all watching.
//  - !ping (to list all) OR !ping [ip] (to start a ping with a specific ip (or host maybe?))
//  - !stop [ip]

type Flags struct {
	ConfigFile string `short:"c" long:"config" description:"configuration file location" default:"config.toml"`
	Debug      bool   `short:"d" long:"debug" description:"enables slack api debugging"`
}

var flags Flags

type Config struct {
	Token   string `toml:"token"`
	Channel string `toml:"channel"`
}

var conf Config
var logger = log.New(os.Stdout, "", log.Lshortfile|log.LstdFlags)
var toSlack = make(chan string, 20)

func main() {
	parser := gflags.NewParser(&flags, gflags.HelpFlag)
	_, err := parser.Parse()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	_, err = toml.DecodeFile(flags.ConfigFile, &conf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	slack.SetLogger(logger)

	if err := newSlack(toSlack); err != nil {
		logger.Fatalln(err)
	}
}
