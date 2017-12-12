package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/goji/httpauth"
)

func httpServer() {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.DefaultCompress)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Throttle(1))
	r.Use(httpauth.SimpleBasicAuth(conf.HTTPUser, conf.HTTPPasswd))

	r.Get(flags.HTTPPrefix+"/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, strings.Replace(`<!DOCTYPE html>
<html lang="en">
	<head>
		<title>ponger</title>
	</head>
	<body style="padding: 20px;">
		<a href="$PREFIX/checks">checks</a><br>
		<a href="$PREFIX/usersettings">user settings</a><br>
		<a href="$PREFIX/slack/conninfo">connection info/slack user list</a><br>
		<a href="$PREFIX/debug">debug</a><br>
	</body>
</html>`, "$PREFIX", flags.HTTPPrefix, -1))
	})

	r.Get(flags.HTTPPrefix+"/checks", func(w http.ResponseWriter, r *http.Request) {
		hostGroup.Lock()
		defer hostGroup.Unlock()

		channelCache.Lock()
		defer channelCache.Unlock()

		JSON(w, r, map[string]interface{}{
			"inv":         hostGroup.inv,
			"maprefcache": channelCache.cache,
		})
	})

	r.Get(flags.HTTPPrefix+"/usersettings", func(w http.ResponseWriter, r *http.Request) { JSON(w, r, GetAllUserSettings()) })
	r.Get(flags.HTTPPrefix+"/slack/conninfo", func(w http.ResponseWriter, r *http.Request) { JSON(w, r, lastConnectInfo) })
	r.Mount(flags.HTTPPrefix+"/debug", middleware.Profiler())

	srv := &http.Server{
		Addr:         flags.HTTP,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 45 * time.Second,
	}

	if err := srv.ListenAndServe(); err != nil {
		panic(err)
	}
}

// JSON marshals 'v' to JSON, automatically escaping HTML and setting the
// Content-Type as application/json.
func JSON(w http.ResponseWriter, r *http.Request, v interface{}) {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(true)
	enc.SetIndent("", "    ")

	if err := enc.Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(buf.Bytes())
}
