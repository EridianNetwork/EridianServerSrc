package photon

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"math/big"
)

var oakleyPrime768 = func() *big.Int {
	p := new(big.Int)
	p.SetBytes([]byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xC9, 0x0F, 0xDA, 0xA2, 0x21, 0x68, 0xC2, 0x34,
		0xC4, 0xC6, 0x62, 0x8B, 0x80, 0xDC, 0x1C, 0xD1, 0x29, 0x02, 0x4E, 0x08, 0x8A, 0x67, 0xCC, 0x74,
		0x02, 0x0B, 0xBE, 0xA6, 0x3B, 0x13, 0x9B, 0x22, 0x51, 0x4A, 0x08, 0x79, 0x8E, 0x34, 0x04, 0xDD,
		0xEF, 0x95, 0x19, 0xB3, 0xCD, 0x3A, 0x43, 0x1B, 0x30, 0x2B, 0x0A, 0x6D, 0xF2, 0x5F, 0x14, 0x37,
		0x4F, 0xE1, 0x35, 0x6D, 0x6D, 0x51, 0xC2, 0x45, 0xE4, 0x85, 0xB5, 0x76, 0x62, 0x5E, 0x7E, 0xC6,
		0xF4, 0x4C, 0x42, 0xE9, 0xA6, 0x3A, 0x36, 0x20, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	})
	return p
}()

var oakleyGenerator768 = big.NewInt(22)

type DiffieHellmanCryptoProvider struct {
	secret    *big.Int
	PublicKey []byte
	SharedKey []byte
}

func NewDiffieHellmanCryptoProvider() *DiffieHellmanCryptoProvider {
	dh := &DiffieHellmanCryptoProvider{}

	secretBytes := make([]byte, 20)
	randomBytes(secretBytes)
	dh.secret = new(big.Int).SetBytes(secretBytes)

	pub := new(big.Int).Exp(oakleyGenerator768, dh.secret, oakleyPrime768)
	dh.PublicKey = dh.bigIntToBytes(pub, 96)

	return dh
}

func (dh *DiffieHellmanCryptoProvider) DeriveSharedKey(clientPublic []byte) {
	clientPub := new(big.Int).SetBytes(clientPublic)
	shared := new(big.Int).Exp(clientPub, dh.secret, oakleyPrime768)

	hash := sha256.Sum256(shared.Bytes())
	dh.SharedKey = hash[:]
}

func (dh *DiffieHellmanCryptoProvider) Encrypt(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(dh.SharedKey)
	if err != nil {
		return nil, err
	}

	blockSize := block.BlockSize()
	padding := blockSize - len(plaintext)%blockSize
	padded := make([]byte, len(plaintext)+padding)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padding)
	}

	iv := make([]byte, blockSize)
	mode := cipher.NewCBCEncrypter(block, iv)
	ciphertext := make([]byte, len(padded))
	mode.CryptBlocks(ciphertext, padded)
	return ciphertext, nil
}

func (dh *DiffieHellmanCryptoProvider) Decrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(dh.SharedKey)
	if err != nil {
		return nil, err
	}

	iv := make([]byte, block.BlockSize())
	mode := cipher.NewCBCDecrypter(block, iv)
	plaintext := make([]byte, len(ciphertext))
	mode.CryptBlocks(plaintext, ciphertext)

	if len(plaintext) == 0 {
		return plaintext, nil
	}
	padding := int(plaintext[len(plaintext)-1])
	if padding > block.BlockSize() || padding > len(plaintext) {
		return plaintext, nil
	}
	return plaintext[:len(plaintext)-padding], nil
}


func (dh *DiffieHellmanCryptoProvider) bigIntToBytes(n *big.Int, length int) []byte {
	b := n.Bytes()
	if len(b) > length && b[0] == 0 {
		b = b[1:]
	}
	if len(b) == length {
		return b
	}
	if len(b) < length {
		padded := make([]byte, length)
		copy(padded[length-len(b):], b)
		return padded
	}
	return b[len(b)-length:]
}
