package board

import (
	"errors"
	"log"
	"math/rand"
)

const SIZE uint8= 25
type Board struct {
	Width uint8
	Height uint8
	array []uint8
}

func New(w,h uint8) *Board {
	return &Board{
		Width: w,
		Height: h,
		array: make([]uint8, int(w)*int(h)),
	}
}

func (b *Board) Snapshot() (col, row uint8, cells[]uint8){
	c := make([]uint8, len(b.array))
	copy(c, b.array)
	return b.Width, b.Height, c
}

func (b *Board) Set(col, row, v uint8) error {
	if col >= b.Width || row >= b.Height {
		return errors.New("out of bounds")
	}
	i := int(row)*int(b.Width) + int(col)
	b.array[i] = v
	return nil
}

func (b *Board) At(col, row uint8) (uint8, error) {
	if col >= b.Width || row >= b.Height {
		return 0, errors.New("out of bounds")
	}
	i := int(row)*int(b.Width) + int(col)
	return b.array[i], nil
}

func (b *Board) FindByID(id uint8) (x, y uint8, ok bool) {
	for y := 0; y < int(b.Height); y++ {
		for x := 0; x < int(b.Width); x++ {
			if b.array[y*int(b.Width)+x] == id {
				return uint8(x), uint8(y), true
			}
		}
	}
	return 0, 0, false
}


func (b *Board) GetNew() (uint8, uint8, error) {
	for {
		x := uint8(rand.Intn(int(b.Width)))
		y := uint8(rand.Intn(int(b.Height)))
		i, err := b.At(x,y)
		if err != nil{
			log.Println("couldnt get new pos for player")
			return 0, 0, err
		}
		if i == 0 {
			return x, y, nil
		}
	}
}
