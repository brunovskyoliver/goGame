package player

type Player struct {
	ID uint8
	X uint8
	Y uint8
}


func New(x,y uint8) *Player{
	return &Player{X: x, Y: y}
}

