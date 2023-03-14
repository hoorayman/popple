package main

import (
	"math/rand"
	"time"

	"github.com/hoorayman/popple/cmd/popcli/app"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	app.Execute()
}
