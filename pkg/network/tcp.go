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
	ModePlayer uint8 = 1
	ModeViewer uint8 = 2
)
const (
	OpMode uint8 = 1
	OpRegister uint8 = 2
	OpPlace uint8 = 3
	OpSnapshot uint8 = 4
	OpMove uint8 = 5
	OpAck uint8 = 6
	OpEcho uint8 = 7
	OpEnd uint8 = 255
)

type PlayerState struct {
	conn net.Conn
	Player *player.Player
	last_echo uint64
	Mode uint8
}
type TCPClient struct {
	conn net.Conn
}
type TCPServer struct {
	listener net.Listener
	mu sync.Mutex
	connToMatch map[net.Conn]*Match
	mm *Matchmaker
	newHandler HandlerFactory
	nextPlayerID uint8
}
type Match struct {
	id	uint8
	handler Handler
	mu sync.Mutex
	players map[net.Conn]*PlayerState
	viewers map[net.Conn]*PlayerState
	idToConn map[uint8]net.Conn
}
type Matchmaker struct {
	mu sync.Mutex
	waiting []net.Conn
	matches []*Match
	MatchNextID uint8
}

type Packet struct {
	Opcode uint8
	X      uint8
	Y      uint8
	W,H	   uint8
	Cells  []byte
	ID	   uint8
	Mode   uint8
}
type Incoming struct {
    Conn net.Conn
    P    Packet
}


type HandlerFactory func() Handler

type Handler interface {
	OnPacket(conn net.Conn, p Packet, id uint8) bool
	Snapshot() (col, row uint8, cells []uint8)
	NewSpawn() (x,y uint8, err error)
	PlaceAt(x,y,v uint8) (err error)
	SpawnPlayer(id uint8) (x, y uint8, err error)

}

func NewMatch(id uint8, h Handler) *Match {
	return &Match{id: id, handler: h, players: make(map[net.Conn]*PlayerState), viewers: make(map[net.Conn]*PlayerState), idToConn: make(map[uint8]net.Conn)}
}

func (mm *Matchmaker) EnqueuePlayer(conn net.Conn) (a,b net.Conn, ok bool){
	mm.mu.Lock()
	defer mm.mu.Unlock()
	mm.waiting = append(mm.waiting, conn)
	if len(mm.waiting) < 2 {
		return nil, nil, false
	}
	a = mm.waiting[0]
	b = mm.waiting[1]
	mm.waiting = mm.waiting[2:]
	return a,b,true

}

func (s *TCPServer) enqueueOrStartMatch(conn net.Conn){
	a, b, ok := s.mm.EnqueuePlayer(conn)
	if !ok {
		return
	}
	s.mm.mu.Lock()
	s.mm.MatchNextID++
	id := s.mm.MatchNextID
	s.mm.mu.Unlock()
	m := NewMatch(id, s.newHandler())
	s.mu.Lock()
	s.connToMatch[a] = m
	s.connToMatch[b] = m
	s.mu.Unlock()
	_ = s.addPlayerToMatch(m, a)
	_ = s.addPlayerToMatch(m,b)
	s.mm.mu.Lock()
	s.mm.matches = append(s.mm.matches, m)
	s.mm.mu.Unlock()
}

func (s *TCPServer) addViewerToMatch(matchID uint8, conn net.Conn) error{
	s.mm.mu.Lock()
	var m *Match
	for _, x := range s.mm.matches {
		if x.id == matchID {
			m = x
			break
		}
	}
	s.mm.mu.Unlock()
	if m == nil {
		return io.EOF
	}
	m.mu.Lock()
	m.viewers[conn] = &PlayerState{
		conn: conn,
		last_echo: 0,
		Mode: ModeViewer,

	}
	m.mu.Unlock()
	s.mu.Lock()
	s.connToMatch[conn] = m
	s.mu.Unlock()
	col, row, cells := m.handler.Snapshot()
	data := append([]byte{OpSnapshot, col, row}, cells...)
	if _, err := conn.Write(data); err != nil{
		log.Println("could not send snapshot")
		s.remove(conn)
		return err
	}
	return nil
}
func (s *TCPServer) addPlayerToMatch(m *Match, conn net.Conn) error{
	s.mu.Lock()
	s.nextPlayerID++
	id := s.nextPlayerID
	s.mu.Unlock()
	x, y, err := m.handler.SpawnPlayer(id)
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
	m.mu.Lock()
	m.players[conn] = &PlayerState{
		conn: conn,
		Player: &player.Player{ID: id, X: x, Y: y},
		last_echo: 0,
		Mode: ModePlayer,

	}
	m.idToConn[id] = conn
	m.mu.Unlock()
	col, row, cells := m.handler.Snapshot()
	data := append([]byte{OpSnapshot, col, row}, cells...)
	if _, err := conn.Write(data); err != nil{
		log.Println("could not send snapshot")
		s.remove(conn)
	}
	return err
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

func NewTCPServer(address string, factory HandlerFactory) (*TCPServer, error) {
	ln, err := net.Listen("tcp4", address)
	if err != nil {
		return nil, err
	}
	return &TCPServer{
		listener:    ln,
		connToMatch: make(map[net.Conn]*Match),
		mm:          &Matchmaker{},
		newHandler:  factory,
	}, nil
}


func (s *TCPServer) Start() error {
	incoming := make(chan Incoming, 32)
	go func() {
		for msg := range incoming {
			s.handleIncoming(msg.Conn, &msg.P)
		}
	}()
	go s.echo()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return err
		}
		op := make([]byte, 1)
		_, err = io.ReadFull(conn, op[:])
		if err != nil {
			log.Printf("error: %s\n", err.Error())
		}
		if op[0] != OpMode {
			log.Printf("not a mode packet")
			conn.Close()
			continue
		}
		mode := make([]byte, 1)
		_, err = io.ReadFull(conn, mode[:])
		if err != nil {
			log.Printf("error: %s\n", err.Error())
			continue
		}
		log.Printf("got mode: %v from: %v", mode[0], conn.RemoteAddr().String())
		switch mode[0]{
		case ModePlayer:
			s.enqueueOrStartMatch(conn)
			go s.readLoop(conn, incoming)
		case ModeViewer:
			match := make([]byte, 1)
			_, err = io.ReadFull(conn, match[:])
			if err != nil {
				log.Printf("error: %s\n", err.Error())
				continue
			}
			m := match[0]
			if err := s.addViewerToMatch(m, conn); err != nil {
				log.Printf("viewer join failed: %v", err)
				_ = conn.Close()
				continue
			}
			go s.readLoop(conn, incoming)
		}
	}
}

func (s *TCPServer) playerByID(id uint8) *PlayerState {
	s.mm.mu.Lock()
	matches := make([]*Match, len(s.mm.matches))
	copy(matches, s.mm.matches)
	s.mm.mu.Unlock()
	for _, m := range matches {
		m.mu.Lock()
		conn, ok := m.idToConn[id]
		if ok {
			ps := m.players[conn]
			m.mu.Unlock()
			return ps
		}
		m.mu.Unlock()
	}
	return nil
}

func (s *TCPServer) handleIncoming(conn net.Conn, p *Packet) {
	m := s.getMatchByConn(conn)
	if m == nil {
		return
	}

	switch p.Opcode {
	case OpEcho:
		m.mu.Lock()
		if ps, ok := m.players[conn]; ok {
			ps.last_echo = 0
		}
		m.mu.Unlock()
		return

	case OpMove:
		m.mu.Lock()
		expected, ok := m.idToConn[p.ID]
		m.mu.Unlock()
		if !ok || expected != conn {
			return
		}

		okMove := m.handler.OnPacket(conn, *p, p.ID)
		if okMove {
			_, _ = conn.Write([]byte{OpAck, p.X, p.Y})
			go s.broadcastMatchSnapshot(m, conn)
		}
	}
}


func (s *TCPServer) echo() {
	ticker := time.NewTicker(1000 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		if len(s.connToMatch) == 0 {
			s.mu.Unlock()
			continue
		}
		conns := make([]net.Conn, 0, len(s.connToMatch))
		for _, m := range s.connToMatch {
			var players []net.Conn

			m.mu.Lock()
			for c := range m.players {
				players = append(players, c)
			}
			m.mu.Unlock()

			for _, c := range players {
				_, _ = c.Write([]byte{OpEcho})
			}

			m.mu.Lock()
			for c, ps := range m.players {
				ps.last_echo++
				if ps.last_echo > 10 { conns = append(conns, c) }
			}
			m.mu.Unlock()
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
func (s *TCPServer) getPlayerStateByConn(conn net.Conn) *PlayerState{
	m := s.connToMatch[conn]
	for _, st := range m.players {
		if st.conn == conn {
			return st
		}
	}
	return nil
}
func (s *TCPServer) remove(conn net.Conn) {
    s.mu.Lock()
    m := s.connToMatch[conn]
    delete(s.connToMatch, conn)
    s.mu.Unlock()

    if m != nil {
		m.mu.Lock()
		st := m.players[conn]
		if st != nil && st.Player != nil {
			delete(m.idToConn, st.Player.ID)
		}
		delete(m.players, conn)
		delete(m.viewers, conn)
		m.mu.Unlock()
        if st != nil && st.Player != nil && st.Mode == ModePlayer {
            _ = m.handler.PlaceAt(st.Player.X, st.Player.Y, 0)
            s.broadcastMatchSnapshot(m, nil)
        }
    }

    _ = conn.Close()
}

func (s *TCPServer) broadcastMatchSnapshot(m *Match, except net.Conn) {
	col, row, cells := m.handler.Snapshot()
	data := append([]byte{OpSnapshot, col, row}, cells...)
	m.mu.Lock()
	conns := make([]net.Conn, 0, len(m.players))
	for conn, _ := range m.players{
		if conn != except {
			conns = append(conns, conn)
		}
	}
	for conn, _ := range m.viewers{
		if conn != except {
			conns = append(conns, conn)
		}
	}
	m.mu.Unlock()
	for _, conn := range conns {
		if _, err := conn.Write(data); err != nil{
			log.Printf("error %s:", err.Error())
			s.remove(conn)
		}
	}
}

func (s *TCPServer) getMatchByConn(conn net.Conn) *Match {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.connToMatch[conn]
	if ok {
		return m
	}
	return nil
}


func (s *TCPServer) readLoop(conn net.Conn, incoming chan<- Incoming){
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
				return
			}
			log.Println("read echo from id:", idByte[0])
			incoming <- Incoming{Conn: conn, P: Packet{Opcode: OpEcho, ID: idByte[0]}}
		case OpMove:
			idByte := make([]byte, 1)
			if _, err := io.ReadFull(conn, idByte[:]); err != nil {
				log.Printf("error reading: %s\n", err.Error())
				return
			}
			id := idByte[0]
			xy := make([]byte, 2)
			if _, err := io.ReadFull(conn, xy[:]); err != nil {
				log.Printf("error reading: %s\n", err.Error())
				return
			}
			p := Packet{Opcode: OpMove, X: xy[0], Y: xy[1], ID: id}
			incoming <- Incoming{Conn: conn, P: p}

		default:
			return

		}
	}
}
