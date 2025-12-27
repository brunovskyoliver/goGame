package main

import (
	"brunovskyoliver/game/pkg/board"
	"brunovskyoliver/game/pkg/network"
	"brunovskyoliver/game/pkg/player"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/jroimartin/gocui"
)

type ClientState struct {
    b    *board.Board
    me   *player.Player
    conn net.Conn
	mode uint8
}


func runUI(st *ClientState, incoming <- chan network.Packet){
	g, err := gocui.NewGui(gocui.Output256)
	if err != nil {
		log.Fatalf("error: %s\n", err.Error())
	}
	defer g.Close()

	g.SetManagerFunc(layout(st))

	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		log.Panicln(err)
	}
	go func() {
		for p := range incoming {
			handlePacket(st, &p)
			g.Update(func(g *gocui.Gui) error {
			return nil})
		}
	}()
	if st.mode == network.ModePlayer {
		if err := g.SetKeybinding("", gocui.KeyArrowUp, gocui.ModNone,
			func(g *gocui.Gui, v *gocui.View) error {
				return moveUp(st, g, v)
			},
		); err != nil {
			log.Panicln(err)
		}
		if err := g.SetKeybinding("", gocui.KeyArrowDown, gocui.ModNone,
			func(g *gocui.Gui, v *gocui.View) error {
				return moveDown(st, g, v)
			},
		); err != nil {
			log.Panicln(err)
		}
		if err := g.SetKeybinding("", gocui.KeyArrowLeft, gocui.ModNone,
			func(g *gocui.Gui, v *gocui.View) error {
				return moveLeft(st, g, v)
			},
		); err != nil {
			log.Panicln(err)
		}
		if err := g.SetKeybinding("", gocui.KeyArrowRight, gocui.ModNone,
			func(g *gocui.Gui, v *gocui.View) error {
				return moveRight(st, g, v)
			},
		); err != nil {
			log.Panicln(err)
		}
	}
	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}
}

func moveUp(st *ClientState, g *gocui.Gui, v *gocui.View) error {
	if st.me == nil {
		return nil
	}
	if st.me.Y == 0 {
		return nil
	}
	x := st.me.X
	y := st.me.Y - 1
	data := []byte{network.OpMove, st.me.ID, x,y}
	_, err := st.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}
func moveDown(st *ClientState, g *gocui.Gui, v *gocui.View) error {
	if st.me == nil {
		return nil
	}
	if st.me.Y == st.b.Height - 1 {
		return nil
	}
	x := st.me.X
	y := st.me.Y + 1
	data := []byte{network.OpMove, st.me.ID, x,y}
	_, err := st.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}
func moveLeft(st *ClientState, g *gocui.Gui, v *gocui.View) error {
	if st.me == nil {
		return nil
	}
	if st.me.X == 0 {
		return nil
	}
	x := st.me.X - 1
	y := st.me.Y
	data := []byte{network.OpMove, st.me.ID, x,y}
	_, err := st.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}
func moveRight(st *ClientState, g *gocui.Gui, v *gocui.View) error {
	if st.me == nil {
		return nil
	}
	if st.me.X == st.b.Width - 1 {
		return nil
	}
	x := st.me.X + 1
	y := st.me.Y
	data := []byte{network.OpMove, st.me.ID, x,y}
	_, err := st.conn.Write(data)
	if err != nil {
		return err
	}
	return nil
}

func readLoop(conn net.Conn, incoming chan <- network.Packet){
	defer close(incoming)
	for {
		op := make([]byte, 1)
		_, err:= io.ReadFull(conn, op[:])
		if err != nil {
			log.Printf("error reading op: %s\n", err.Error())
			return
		}
		switch op[0]{
		case network.OpEcho:
			incoming <- network.Packet{Opcode: network.OpEcho}
		case network.OpRegister:
			var b [3]byte
			if _, err := io.ReadFull(conn, b[:]); err != nil {
				log.Printf("error reading register: %s\n", err.Error())
				return
			}

			incoming <- network.Packet{
				Opcode: network.OpRegister,
				ID:     b[0],
				X:      b[1],
				Y:      b[2],
			}

		case network.OpSnapshot:
			var wh [2]byte
			_, err:= io.ReadFull(conn, wh[:])
			if err != nil {
				log.Printf("error reading width + height: %s\n", err.Error())
				return
			}
			w, h := wh[0], wh[1]
			cells := make([]byte, int(w)*int(h))
			_, err = io.ReadFull(conn, cells)
			if err != nil {
				log.Printf("error: %s\n", err.Error())
				return
			}
			incoming <- network.Packet{Opcode: network.OpSnapshot, W: w, H: h, Cells: cells}
		case network.OpPlace:
			var xy [2]byte
			_, err:= io.ReadFull(conn, xy[:])
			if err != nil {
				log.Printf("error reading x and y for place: %s\n", err.Error())
				return
			}
			incoming <- network.Packet{Opcode: network.OpPlace, X: xy[0], Y: xy[1]}
		case network.OpAck:
			var xy [2]byte
			_, err:= io.ReadFull(conn, xy[:])
			if err != nil {
				log.Printf("error reading x and y for place: %s\n", err.Error())
				return
			}
			incoming <- network.Packet{Opcode: network.OpAck, X: xy[0], Y: xy[1]}
		default:
			log.Println("unknown op:", op)
			return
		}
	}
}

func applySnapshot(b *board.Board, w, h uint8, cells []byte){
	for y:= uint8(0); y < h; y++{
		for x:=uint8(0); x < w; x++{
			v:=cells[int(y)*int(w)+int(x)]
			_ = b.Set(x,y,v)
		}
	}
}

func main() {
	var ip, mode string
	flag.StringVar(&ip, "ip", "127.0.0.1", "ip address of the server")
	flag.StringVar(&mode, "mode", "p", "mode of the game")
	flag.Parse()
	ip = net.JoinHostPort(ip, "8088")
	b := board.New(board.SIZE, board.SIZE)
	ticker := time.NewTicker(1000 * time.Millisecond)
	var conn net.Conn
	for range ticker.C {
		c, err := net.Dial("tcp4", ip)
		if err != nil {
			log.Println("couldnt connect", err)
			continue
		}
		conn = c
		break
	}
	defer conn.Close()
	modeInt := network.ModePlayer
	if mode == "v" {
		modeInt = network.ModeViewer
	}
	p := network.Packet{Opcode: network.OpMode, Mode: modeInt}
	data := []byte{p.Opcode, p.Mode}
	_, err := conn.Write(data)
	if err != nil {
		log.Printf("error: %s\n", err.Error())
		return
	}
	st := &ClientState{
		b: b,
		conn: conn,
		mode: modeInt,
	}
	incoming := make(chan network.Packet, 32)
	go readLoop(conn, incoming)
	runUI(st,incoming)
}

func handlePacket(st *ClientState, p *network.Packet) {
	switch p.Opcode{
	case network.OpEcho:
		p := network.Packet{Opcode: network.OpEcho, ID: 0}
		if st.mode == network.ModePlayer {
			p.ID = st.me.ID
		}
		data := []byte{p.Opcode,p.ID}
		_, err := st.conn.Write(data)
		if err != nil {
			log.Printf("error could not send echo back: %s\n", err.Error())
		}
	case network.OpPlace:
		if err := st.b.Set(p.X, p.Y, 255); err != nil {
			log.Println("Set failed:", err)
		}
	case network.OpSnapshot:
		applySnapshot(st.b, p.W, p.H, p.Cells)
	case network.OpRegister:
		log.Printf("registered as id=%d at (%d,%d)", p.ID, p.X, p.Y)
		st.me = &player.Player{ID: p.ID, X: p.X, Y: p.Y}
	case network.OpAck:
		if st.mode == network.ModePlayer {
			st.b.Set(st.me.X, st.me.Y, 0)
			x, y := p.X, p.Y
			st.me.X, st.me.Y = x,y
			st.b.Set(x,y,st.me.ID)
		}
	}
}


func layout(st *ClientState) func(*gocui.Gui) error {
	return func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		sz := int(board.SIZE)
		cellW := 2
		boardW := sz * cellW
		left  := maxX/2 - boardW/2 - cellW
		right := left + boardW + 2
		top := maxY/2 - sz/2
		bottom := top + sz + 1
		headerTop := top - 2

		hv, err := g.SetView("header", left, headerTop, right, headerTop+2)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		hv.Frame = false
		hv.Clear()

		title := ""
		if st.me != nil {
			title = fmt.Sprintf("PLAYER %d", st.me.ID)
		}

		w := right - left - 1
		pad := (w - len(title)) / 2
		pad = max(0, pad)
		fmt.Fprintf(hv, "%*s%s", pad, "", title)
		v, err := g.SetView("board", left, top, right, bottom)
		if err != nil && err != gocui.ErrUnknownView {
			return err
		}
		v.Wrap = false
		v.Clear()

		for y := 0; y < sz; y++ {
			fmt.Fprint(v," ")
			for x := 0; x < sz; x++ {
				val, _ := st.b.At(uint8(x), uint8(y))

				switch {
				case val == 0:
					fmt.Fprint(v, "\x1b[90m·\x1b[0m ")
				case st.me != nil && val == st.me.ID:
					fmt.Fprint(v, "\x1b[32m@\x1b[0m ")
				case val == 255:
					fmt.Fprint(v, "\x1b[31m#\x1b[0m ")
				default:
					fmt.Fprint(v, "\x1b[36mO\x1b[0m ")
				}
			}
			fmt.Fprintln(v)
		}

		return nil
	}
}



func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

