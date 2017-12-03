package surf

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

func generateID() string {
	return <-randomID
}

var randomID chan string

func init() {
	randomID = make(chan string, 16)
	go func() {
		b := make([]byte, 16)
		for {
			if _, err := rand.Read(b); err != nil {
				time.Sleep(time.Second)
				continue
			}
			randomID <- hex.EncodeToString(b)
		}
	}()

}
