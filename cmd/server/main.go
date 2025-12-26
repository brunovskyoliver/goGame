package main

import (
	"brunovskyoliver/game/pkg/board"
	"brunovskyoliver/game/pkg/network"
	"log"
	"net"
	"sync"
)

type GameState struct {
	mu sync.Mutex
	board *board.Board
}

func (g *GameState) OnPacket(conn net.Conn, p network.Packet, id uint8) bool{
	g.mu.Lock()
	defer g.mu.Unlock()
	switch p.Opcode {
	case network.OpMove:
		x, y := p.X, p.Y
		val, err := g.board.At(x,y)
		if err != nil {
			log.Printf("error: %s\n", err.Error())
			return false
		}
		if val == 0{
			col, row, ok := g.board.FindByID(id)
			if !ok {
				log.Printf("error couldnt get idx of the client\n")
				return false
			}
			g.board.Set(col, row, 0)
			g.board.Set(x,y, id)
			return true
			} else {
			return false
		}
	}
	_ = g.board.Set(p.X, p.Y, 255)
	return true
}
func (g *GameState) Snapshot() (col, row uint8, cells []uint8){
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.board.Snapshot()
}
func (g *GameState) NewSpawn() (x, y uint8, err error){
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.board.GetNew()
}
func (g *GameState) PlaceAt(x,y,v uint8) (err error){
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.board.Set(x,y,v)
}
func (g *GameState) SpawnPlayer(id uint8) (x, y uint8, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	x, y, err = g.board.GetNew()
	if err != nil {
		return 0, 0, err
	}
	if err := g.board.Set(x, y, id); err != nil {
		return 0, 0, err
	}
	return x, y, nil
}


func main(){
	g := &GameState{board: board.New(board.SIZE, board.SIZE)}
	server, err := network.NewTCPServer(":8088", g)
	if err != nil {
		log.Fatalf("error: %s\n", err.Error())
	}
	_ = server.Start()
}
