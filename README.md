<h1>
  <img src="https://raw.githubusercontent.com/hoorayman/popple/main/logo.png" align="left" height="100px" alt="Popple logo"/>
  <span>Popple</span>
</h1>

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://github.com/hoorayman/popple/blob/main/LICENSE)

Popple is a distributed, highly available, general purpose key/value database.

Features
========

- In-memory database for fast reads and writes using b-tree
- Embeddable with a simple HTTP API
- ACID semantics with locking transactions that support rollbacks
- A flexible key/value store enables storing dynamic data, like redis sds
- Ensure high availability and consistency using the Raft protocol
- Simple to use command line or optional configuration file
- Grpc and http serve the same port

Quick Start
===============

## Installing

To start using Popple, install Go and run `make build` in root source folder.
This will generate an executable file named popple in ./bin folder.
You can move it to any where in your `PATH`.

## Usage

Run `popple --help` or `popple server --help` to view the usage. For example:

```sh
Start a Popple server

Usage:
  popple server [flags]

Flags:
      --cluster string    Define cluster servers. The format is sid:address pairs, comma separation. For example, "0=127.0.0.1:8876,1=192.168.0.2:8889,2=192.168.0.56:8080"
  -c, --config string     Path to a configuration file.
      --data-dir string   Path to raft log and database file dir. (default "./")
      --dev               Enable development mode. In this mode, Popple runs in-memory and starts. As the name implies, do not run "dev" mode in production. The default is false.
  -h, --help              help for server
      --sid int           Current Server ID. default is 0
```

The command line arguments are simple and straightforward.
For example: To run a single node dev mode server, you just run `popple server --dev`. The server will select a random port to serve. You can get the port from console:

```sh
2023/02/21 11:17:55 DevMode: true
2023/02/21 11:17:55 server[0] listening at [::]:36967

...
```

## Testing

Run `make test` in root source folder to view high availability on conditions like leader down, network partition...And so forth.

How to contact?
===============

- Mail: hoorayman@126.com
- 今日头条: 浩仔浩仔

Contribute
===============

For Popple, if you have any questions, welcome to issue, or you can also directly start Pull Requests. Thank you for your interest in contributing!
