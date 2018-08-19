package surf

import (
	"crypto/rand"
	"encoding/hex"
)

func generateID() string {
	if id, ok := <-randomID; ok {
		return id
	}
	panic("cannot generate id")
}

var randomID chan string

func init() {
	randomID = make(chan string, 16)
	go func() {
		b := make([]byte, 16)
		for {
			if _, err := rand.Read(b); err != nil {
				close(randomID)
				panic("cannot read random data: " + err.Error())
			}
			randomID <- hex.EncodeToString(b)
		}
	}()

}
