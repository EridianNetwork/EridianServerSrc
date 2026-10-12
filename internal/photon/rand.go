package photon

import "crypto/rand"

func randomBytes(buf []byte) {
	rand.Read(buf)
}
