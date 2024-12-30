package str

func DecodeHex(encoded []byte) []byte {
	decoded := make([]byte, 0, len(encoded)/2)
	for i := 0; i < len(encoded); i += 2 {
		b := byte(0)
		for j := 0; j < 2; j++ {
			b *= 16
			c := encoded[i+j]
			if c >= '0' && c <= '9' {
				b += byte(c - '0')
			} else if c >= 'A' && c <= 'Z' {
				b += byte(c - 55)
			} else if c >= 'a' && c <= 'z' {
				b += byte(c - 87)
			}
		}
		decoded = append(decoded, b)
	}
	return decoded
}

var b64Alphabet = []byte(
	"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/")

var b64AlphabetReverse = func() map[byte]uint32 {
	reversed := make(map[byte]uint32, len(b64Alphabet))
	for i, c := range b64Alphabet {
		reversed[c] = uint32(i)
	}
	return reversed
}()

func DecodeB64(data string) []byte {
	size := len(data)
	pad := 0
	for i := size; data[i-1] == '='; i-- {
		pad++
	}
	decoded := make([]byte, 0, size/4*3)
	for i := 0; i < size/4; i++ {
		j := i * 4
		b0 := b64AlphabetReverse[data[j]]
		b1 := b64AlphabetReverse[data[j+1]]
		b2 := b64AlphabetReverse[data[j+2]]
		b3 := b64AlphabetReverse[data[j+3]]
		n := b0<<18 | b1<<12 | b2<<6 | b3
		decoded = append(decoded, byte(n>>16), byte(n>>8), byte(n))
	}
	return decoded[0 : len(decoded)-pad]
}

func EncodeB64(data []byte) string {
	size := len(data)
	encoded := make([]byte, 0, (size/3+1)*4)
	for i := 0; i < size/3; i++ {
		j := i * 3
		n := uint(data[j])<<16 | uint(data[j+1])<<8 | uint(data[j+2])
		encoded = append(
			encoded,
			b64Alphabet[n>>18&0b00111111],
			b64Alphabet[n>>12&0b00111111],
			b64Alphabet[n>>6&0b00111111],
			b64Alphabet[n&0b00111111])
	}
	return string(encodeB64LastBlock(data, size, encoded))
}

func encodeB64LastBlock(data []byte, size int, encoded []byte) []byte {
	rem := size % 3
	if rem == 0 {
		return encoded
	}
	n := uint(data[size-rem]) << 16
	if rem == 2 {
		n |= uint(data[size-rem+1]) << 8
	}
	encoded = append(
		encoded,
		b64Alphabet[n>>18&0b00111111],
		b64Alphabet[n>>12&0b00111111])
	if rem == 2 {
		encoded = append(encoded, b64Alphabet[n>>6&0b00111111])
	}
	for i := 0; i < 3-rem; i++ {
		encoded = append(encoded, '=')
	}
	return encoded
}
