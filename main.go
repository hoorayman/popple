package main

import (
	"math/rand"
	"time"

	"github.com/hoorayman/popple/cmd"
)

func main() {
	rand.Seed(time.Now().UnixNano())
	cmd.Execute()
}
