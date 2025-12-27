package network
//

import (
	"brunovskyoliver/game/pkg/player"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

const (
	OpPlace uint8 = 1
	OpRegister uint8 = 2
	OpSnapshot uint8 = 3
	OpMove uint8 = 4
	OpAck uint8 = 5
	OpEcho uint8 = 6
	OpEnd uint8 = 255
)
type PlayerState struct {
	conn net.Conn
	Player *player.Player
	last_echo uint64
}
type TCPClient struct {
	conn net.Conn
}
type TCPServer struct {
	listener net.Listener
	handler  Handler
	mu sync.Mutex
	clients map[net.Conn]*PlayerState
	broadcastStarted bool
	nextPlayerID uint8
}

type Packet struct {
	Opcode uint8
	X      uint8
	Y      uint8
	W,H	   uint8
	Cells  []byte
	ID	   uint8
}

type Handler interface {
	OnPacket(conn net.Conn, p Packet, id uint8) bool
	Snapshot() (col, row uint8, cells []uint8)
	NewSpawn() (x,y uint8, err error)
	PlaceAt(x,y,v uint8) (err error)
	SpawnPlayer(id uint8) (x, y uint8, err error)

}

func NewTCPClient(address string) (*TCPClient, error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return &TCPClient{conn: conn}, nil
}

func (c TCPClient) Conn() net.Conn {
	return c.conn
}

func (tcp *TCPClient) Send(data []byte) error {
	_, err := tcp.conn.Write(data)
	return err
}

func (tcp *TCPClient) Close() error {
	return tcp.conn.Close()
}

func NewTCPServer(address string, h Handler) (*TCPServer, error) {
	ln, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return &TCPServer{listener: ln, handler: h, clients: make(map[net.Conn]*PlayerState)}, nil
}

func (s *TCPServer) Start() error {
	incoming := make(chan Packet, 32)
	go func() {
		for p := range incoming {
			s.handlePacket(&p)
		}
	}()
	go s.echo()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		err = s.register(conn)
		if err != nil {
			log.Printf("error: %s\n", err.Error())
			continue
		}
		go s.readLoop(conn, incoming)
	}
}

func (s *TCPServer) playerByID(id uint8) *PlayerState{
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ps := range s.clients{
		if ps.Player.ID == id {
			return ps
		}

	}
	return nil
}

func (s *TCPServer) register(conn net.Conn) error{
	s.mu.Lock()
	s.nextPlayerID++
	id := s.nextPlayerID
	startBroadcast := !s.broadcastStarted
	if startBroadcast {
		s.broadcastStarted = true
	}
	s.mu.Unlock()
	x, y, err := s.handler.SpawnPlayer(id)
	if err != nil {
		log.Printf("error could not spawn: %s\n", err.Error())
		_ = conn.Close()
		return err
	}
	reg := []byte{OpRegister, id, x, y}
	if _, err := conn.Write(reg); err != nil {
		log.Println("could not send register:", err)
		s.remove(conn)
		return err
	}
	s.mu.Lock()
	s.clients[conn] = &PlayerState{
		conn: conn,
		Player: &player.Player{ID: id, X: x, Y: y},
		last_echo: 0,
	}
	s.mu.Unlock()
	if startBroadcast {
		go s.broadcastLoop()
	}
	col, row, cells := s.handler.Snapshot()
	data := append([]byte{OpSnapshot, col, row}, cells...)
	if _, err := conn.Write(data); err != nil{
		log.Println("could not send snapshot")
		s.remove(conn)
		return err
	}
	s.broadcastSnapshot(conn)
	return nil
}
func (s *TCPServer) echo() {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		if len(s.clients) == 0 {
			s.mu.Unlock()
			continue
		}
		conns := make([]net.Conn, 0, len(s.clients))
		for c, ps := range s.clients{
			if ps.last_echo > uint64(10){
				conns = append(conns, c)
				log.Printf("echo not recieved from %v in last 10s", ps.Player.ID)
			} else {
				ps.last_echo++
			}
		}
		s.mu.Unlock()
		for _ , c := range conns {
			p := Packet{Opcode: OpEnd}
			data := []byte{p.Opcode}
			_, err := c.Write(data)
			if err != nil {
				log.Printf("error: %s\n", err.Error())
				continue
			}
			s.remove(c)
		}
	}
}

func (s *TCPServer) handlePacket(p *Packet){
	switch p.Opcode{
	case OpEcho:
		id := p.ID
		if st := s.playerByID(id); st == nil {
			log.Printf("could not find playerState for the id: %v\n", id)
		} else {
			s.mu.Lock()
			st.last_echo = 0
			s.mu.Unlock()
		}

	}

}

func (s *TCPServer) remove(conn net.Conn) {
	var st *PlayerState
	var ok bool
	s.mu.Lock()
	st, ok = s.clients[conn]
	if ok {
		delete(s.clients, conn)
	}
	if len(s.clients) == 0 {
		s.broadcastStarted = false
	}
	s.mu.Unlock()
	if ok && st != nil && st.Player != nil{
		_ = s.handler.PlaceAt(st.Player.X, st.Player.Y, 0)
		s.broadcastSnapshot(nil)
	}
	_ = conn.Close()
}

func (s *TCPServer) broadcastLoop() {
	ticker := time.NewTicker(2000 * time.Millisecond)
	defer ticker.Stop()

	for range ticker.C {
		s.mu.Lock()
		if len(s.clients) == 0 {
			s.broadcastStarted = false
			s.mu.Unlock()
			return
		}

		conns := make([]net.Conn, 0, len(s.clients))
		for c := range s.clients {
			conns = append(conns, c)
		}
		s.mu.Unlock()
		p := Packet{Opcode:OpEcho}
		data := []byte{p.Opcode}
		for _, c := range conns {
			if _, err := c.Write(data); err != nil {
				s.remove(c)
			}
		}
	}
}

func (s *TCPServer) broadcastSnapshot(except net.Conn) {
	col, row, cells := s.handler.Snapshot()
	data := append([]byte{OpSnapshot, col, row}, cells...)
	s.mu.Lock()
	conns := make([]net.Conn, 0, len(s.clients))
	for _, conn := range s.clients{
		if conn.conn != except {
			conns = append(conns, conn.conn)
		}
	}
	s.mu.Unlock()
	for _, conn := range conns {
		if _, err := conn.Write(data); err != nil{
			log.Printf("error %s:", err.Error())
			s.remove(conn)
		}
	}
}


func (s *TCPServer) readLoop(conn net.Conn, incoming chan <- Packet) {
	defer s.remove(conn)
	for {
		op := make([]byte, 1)
		if _, err := io.ReadFull(conn, op[:]); err != nil {
			log.Printf("error reading: %s\n", err.Error())
			return
		}
		switch op[0]{
		case OpEcho:
			idByte := make([]byte, 1)
			if _, err := io.ReadFull(conn, idByte[:]); err != nil {
				log.Printf("error reading: %s\n", err.Error())
				continue
			}
			log.Println("read echo from id:", idByte[0])
			incoming <- Packet{Opcode: OpEcho, ID: idByte[0]}
		case OpMove:
			idByte := make([]byte, 1)
			if _, err := io.ReadFull(conn, idByte[:]); err != nil {
				log.Printf("error reading: %s\n", err.Error())
				continue
			}
			id := idByte[0]
			xy := make([]byte, 2)
			if _, err := io.ReadFull(conn, xy[:]); err != nil {
				log.Printf("error reading: %s\n", err.Error())
				continue
			}
			p := Packet{Opcode: OpMove, X: xy[0], Y: xy[1], ID: id}
			log.Printf("got packet from %v: %v, %v", id, xy[0], xy[1])
			val := s.handler.OnPacket(conn, p, id)
			if val {
				data := append([]byte{OpAck}, xy[:]...)
				_, err := conn.Write(data)
				if err != nil {
					log.Printf("error: %s\n", err.Error())
					continue
				}
				go s.broadcastSnapshot(conn)
			}
		default:
			return

		}
	}
}
