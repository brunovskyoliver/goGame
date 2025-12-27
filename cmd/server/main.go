package main

import (
	"brunovskyoliver/game/pkg/game"
	"brunovskyoliver/game/pkg/network"
	"log"
)



func main(){
	server, err := network.NewTCPServer("0.0.0.0:8088", func() network.Handler {return game.NewGameState()})
	if err != nil {
		log.Fatalf("error: %s\n", err.Error())
	}
	_ = server.Start()
}
