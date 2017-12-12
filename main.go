package main

import (
	"fmt"
	"log"
	"os"

	"github.com/BurntSushi/toml"
	gflags "github.com/jessevdk/go-flags"
	"github.com/nlopes/slack"
	"github.com/paulstuart/ping"
)

type Flags struct {
	ConfigFile string `short:"c" long:"config" description:"configuration file location" default:"config.toml"`
	Debug      bool   `short:"d" long:"debug" description:"enables slack api debugging"`
	UserDB     string `long:"user-db" description:"path to user settings database file" default:"user_settings.db"`
	HTTP       string `long:"http" description:"address/port to bind to" default:":8080"`
	HTTPPrefix string `long:"http-prefix" description:"prefix uri for the http server (e.g. if behind a proxy)"`
	Ping       string `long:"ping" short:"p" description:"test the ping functionality builtin to ponger"`
}

var flags Flags

type Config struct {
	Token           string `toml:"token"`
	IncomingChannel string `toml:"incoming_channel"`
	RemovalTimeout  int    `toml:"removal_timeout_secs"`
	ForcedTimeout   int    `toml:"forced_timeout_secs"`
	NotifyOnStart   bool   `toml:"notify_on_start"`
	ReactionTrigger string `toml:"reaction_trigger"`
	HTTPUser        string `toml:"http_user"`
	HTTPPasswd      string `toml:"http_password"`
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

	if flags.Ping != "" {
		err = ping.Pinger(flags.Ping, 2)
		if err == nil {
			logger.Println("PING OK")
			os.Exit(0)
		}

		logger.Printf("PING FAIL: %s", err)
		os.Exit(1)
	}

	_, err = toml.DecodeFile(flags.ConfigFile, &conf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}

	if conf.RemovalTimeout < 120 {
		conf.RemovalTimeout = 120
	}

	if conf.ForcedTimeout < 240 {
		conf.ForcedTimeout = 240
	}

	slack.SetLogger(logger)

	go httpServer()

	if err := newSlackRTM(toSlack); err != nil {
		logger.Fatalln(err)
	}
}
