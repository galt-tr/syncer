// Package buxAgent is Golang agent for interacting with the BitCoin node
//
// If you have any suggestions or comments, please feel free to open an issue on
// this GitHub repository!
//
// By BuxOrg (https://github.com/BuxOrg)
package main

import "github.com/BuxOrg/bux-agent/server"

func main() {
	server.Start()
}

func Greet() string {
	return "Hello!"
}
