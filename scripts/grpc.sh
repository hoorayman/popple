#!/usr/bin/env bash

#buf mod update
buf lint
buf breaking --against .git
buf generate
