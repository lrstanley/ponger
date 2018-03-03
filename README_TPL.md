<p align="center">ponger -- Server monitoring and reporting bot for Slack</p>
<p align="center">
  <a href="https://travis-ci.org/lrstanley/ponger"><img src="https://travis-ci.org/lrstanley/ponger.svg?branch=master" alt="Build Status"></a>
  <a href="https://byteirc.org/channel/%23%2Fdev%2Fnull"><img src="https://img.shields.io/badge/ByteIRC-%23%2Fdev%2Fnull-blue.svg" alt="IRC Chat"></a>
</p>

## Table of Contents
- [Installation](#installation)
  - [Ubuntu/Debian](#ubuntudebian)
  - [CentOS/Redhat](#centosredhat)
  - [Manual Install](#manual-install)
  - [Build from source](#build-from-source)
- [Usage](#usage)
  - [Example](#example)
- [Contributing](#contributing)
- [License](#license)

## Installation

Check out the [releases](https://github.com/lrstanley/ponger/releases)
page for prebuilt versions. Below are example commands of how you would install
the utility.

### Ubuntu/Debian

```console
$ wget https://liam.sh/ghr/ponger_[[tag]]_[[os]]_[[arch]].deb
$ dpkg -i ponger_[[tag]]_[[os]]_[[arch]].deb
```

### CentOS/Redhat

```console
$ yum localinstall https://liam.sh/ghr/ponger_[[tag]]_[[os]]_[[arch]].rpm
```

### Manual Install

```console
$ wget https://liam.sh/ghr/ponger_[[tag]]_[[os]]_[[arch]].tar.gz
$ tar -C /usr/bin/ -xzvf ponger_[[tag]]_[[os]]_[[arch]].tar.gz ponger
$ chmod +x /usr/bin/ponger
```

### Source

Note that you must have [Go](https://golang.org/doc/install) installed and
a fully working `$GOPATH` setup.

```console
$ go get -d -u github.com/lrstanley/ponger
$ cd $GOPATH/src/github.com/lrstanley/ponger
$ make
$ ./ponger --help
```

## Usage

```console
$ ponger --help
Usage:
  ponger [OPTIONS]

Application Options:
  -c, --config=      configuration file location (default: config.toml)
  -d, --debug        enables slack api debugging
      --user-db=     path to user settings database file (default: user_settings.db)
      --http=        address/port to bind to (default: :8080)
      --http-prefix= prefix uri for the http server (e.g. if behind a proxy)
  -p, --ping=        test the ping functionality builtin to ponger

Help Options:
  -h, --help         Show this help message
```

### Example

```console
$ ponger -c yourconf.toml -d --http "localhost:8080"
$ ponger -c yourconf.toml -p "8.8.8.8"
```

## Contributing

Below are a few guidelines if you would like to contribute. Keep the code
clean, standardized, and much of the quality should match Golang's standard
library and common idioms.

   * Always test using the latest Go version.
   * Always use `gofmt` before committing anything.
   * Always have proper documentation before committing.
   * Keep the same whitespacing, documentation, and newline format as the
     rest of the project.
   * Only use 3rd party libraries if necessary. If only a small portion of
     the library is needed, simply rewrite it within the library to prevent
     useless imports.
   * Also see [golang/go/wiki/CodeReviewComments](https://github.com/golang/go/wiki/CodeReviewComments)

## License

```
LICENSE: The MIT License (MIT)
Copyright (c) 2017 Liam Stanley <me@liamstanley.io>

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
