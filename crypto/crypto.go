package crypto

import (
	"syscall"

	"github.com/alaisi/syscalltodo/io"
)

func HmacSha256(key []byte, data []byte) []byte {
	ipad := [64]byte{}
	opad := [64]byte{}
	for i := 0; i < 64; i++ {
		b := byte(0)
		if i < len(key) {
			b = key[i]
		}
		ipad[i] = b ^ 0x36
		opad[i] = b ^ 0x5c
	}
	firstPass := make([]byte, 0, len(ipad)+len(data))
	firstPass = append(firstPass, ipad[:]...)
	firstPass = append(firstPass, data...)
	secondPass := make([]byte, 0, len(opad)+32)
	secondPass = append(secondPass, opad[:]...)
	secondPass = append(secondPass, Sha256(firstPass)...)
	return Sha256(secondPass)
}

func Pbkdf2HmacSha256(password []byte, salt []byte, iterations int) []byte {
	initialSalt := make([]byte, 0, len(salt)+4)
	initialSalt = append(initialSalt, salt...)
	initialSalt = append(initialSalt, 0, 0, 0, 1)
	key := HmacSha256(password, initialSalt)
	for i, prev := 1, key; i < iterations; i++ {
		prev = HmacSha256(password, prev)
		for j := 0; j < len(key); j++ {
			key[j] ^= prev[j]
		}
	}
	return key
}

func Rand(b []byte) error {
	fd, err := syscall.Open("/dev/random", syscall.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer syscall.Close(fd)
	_, err = io.Read(fd, b)
	return err
}

func memcpy32(dst32 []uint32, offset uint, src []byte, size uint) {
	start32 := offset / 4
	end32 := start32 + size/4 + 1
	dst := make([]byte, (end32-start32)*4)
	dst[0] = byte(dst32[start32])
	dst[1] = byte(dst32[start32] >> 8)
	dst[2] = byte(dst32[start32] >> 16)
	dst[3] = byte(dst32[start32] >> 24)
	last := (end32 - start32 - 1) * 4
	dst[last] = byte(dst32[end32-1])
	dst[last+1] = byte(dst32[end32-1] >> 8)
	dst[last+2] = byte(dst32[end32-1] >> 16)
	dst[last+3] = byte(dst32[end32-1] >> 24)
	copy(dst[offset%4:], src[0:size])
	for i, j := start32, 0; i < end32; i++ {
		dst32[i] = uint32(dst[j]) |
			uint32(dst[j+1])<<8 |
			uint32(dst[j+2])<<16 |
			uint32(dst[j+3])<<24
		j += 4
	}
}

func Sha256(data []byte) []byte {
	output := make([]byte, 32)
	sha256(data, uint64(len(data)), output)
	return output
}

// sha256 ported from Kent Williams-Kings public domain C implementation:
//   https://github.com/etherealvisage/sha256/blob/master/sha256.c

var sha256_initial_h = [8]uint32{
	0x6a09e667, 0xbb67ae85, 0x3c6ef372, 0xa54ff53a,
	0x510e527f, 0x9b05688c, 0x1f83d9ab, 0x5be0cd19}

var sha256_round_k = [64]uint32{
	0x428a2f98, 0x71374491, 0xb5c0fbcf, 0xe9b5dba5, 0x3956c25b, 0x59f111f1,
	0x923f82a4, 0xab1c5ed5, 0xd807aa98, 0x12835b01, 0x243185be, 0x550c7dc3,
	0x72be5d74, 0x80deb1fe, 0x9bdc06a7, 0xc19bf174, 0xe49b69c1, 0xefbe4786,
	0x0fc19dc6, 0x240ca1cc, 0x2de92c6f, 0x4a7484aa, 0x5cb0a9dc, 0x76f988da,
	0x983e5152, 0xa831c66d, 0xb00327c8, 0xbf597fc7, 0xc6e00bf3, 0xd5a79147,
	0x06ca6351, 0x14292967, 0x27b70a85, 0x2e1b2138, 0x4d2c6dfc, 0x53380d13,
	0x650a7354, 0x766a0abb, 0x81c2c92e, 0x92722c85, 0xa2bfe8a1, 0xa81a664b,
	0xc24b8b70, 0xc76c51a3, 0xd192e819, 0xd6990624, 0xf40e3585, 0x106aa070,
	0x19a4c116, 0x1e376c08, 0x2748774c, 0x34b0bcb5, 0x391c0cb3, 0x4ed8aa4a,
	0x5b9cca4f, 0x682e6ff3, 0x748f82ee, 0x78a5636f, 0x84c87814, 0x8cc70208,
	0x90befffa, 0xa4506ceb, 0xbef9a3f7, 0xc67178f2}

func sha256_endian_reverse64(input uint64, output []byte) {
	output[7] = byte(input >> 0)
	output[6] = byte(input >> 8)
	output[5] = byte(input >> 16)
	output[4] = byte(input >> 24)
	output[3] = byte(input >> 32)
	output[2] = byte(input >> 40)
	output[1] = byte(input >> 48)
	output[0] = byte(input >> 56)
}
func sha256_endian_read32(input []byte) uint32 {
	return uint32(input[0])<<24 |
		uint32(input[1])<<16 |
		uint32(input[2])<<8 |
		uint32(input[3])
}
func sha256_endian_read32_i(input uint32) uint32 {
	return sha256_endian_read32([]byte{
		byte(input), byte(input >> 8), byte(input >> 16), byte(input >> 24)})
}
func sha256_endian_reverse32(input uint32, output []byte) {
	output[3] = byte(input >> 0)
	output[2] = byte(input >> 8)
	output[1] = byte(input >> 16)
	output[0] = byte(input >> 24)
}
func sha256_ror(input uint32, by uint32) uint32 {
	return (input >> by) | ((input & ((1 << by) - 1)) << (32 - by))
}
func sha256(data []byte, len uint64, output []byte) {
	padding := [80]byte{}
	current := len + 1%64
	needed := (64 + 56 - current) % 64
	extra := needed + 9
	total := len + extra
	padding[0] = 0x80
	sha256_endian_reverse64(len*8, padding[total-len-8:])
	v := [8]uint32{}
	for i := 0; i < 8; i++ {
		v[i] = sha256_initial_h[i]
	}
	for cursor := uint64(0); cursor*64 < total; cursor++ {
		t := [8]uint32{}
		copy(t[:], v[:])
		w := [64]uint32{}
		if cursor*64+64 <= len {
			for j := uint64(0); j < 16; j++ {
				w[j] = sha256_endian_read32(data[cursor*64+j*4:])
			}
		} else {
			if cursor*64 < len {
				size := uint(len - cursor*64)
				if size > 0 {
					memcpy32(w[:], 0, data[cursor*64:], size)
				}
				memcpy32(w[:], size, padding[:], 64-size)
			} else {
				off := (cursor*64 - len) % 64
				memcpy32(w[:], 0, padding[off:], 64)
			}
			for j := 0; j < 16; j++ {
				w[j] = sha256_endian_read32_i(w[j])
			}
		}
		for j := 16; j < 64; j++ {
			s1 := sha256_ror(w[j-2], 17) ^ sha256_ror(w[j-2], 19) ^ (w[j-2] >> 10)
			s0 := sha256_ror(w[j-15], 7) ^ sha256_ror(w[j-15], 18) ^ (w[j-15] >> 3)
			w[j] = s1 + w[j-7] + s0 + w[j-16]
		}
		for j := 0; j < 64; j++ {
			ch := (t[4] & t[5]) ^ (^t[4] & t[6])
			maj := (t[0] & t[1]) ^ (t[0] & t[2]) ^ (t[1] & t[2])
			S0 := sha256_ror(t[0], 2) ^ sha256_ror(t[0], 13) ^ sha256_ror(t[0], 22)
			S1 := sha256_ror(t[4], 6) ^ sha256_ror(t[4], 11) ^ sha256_ror(t[4], 25)
			t1 := t[7] + S1 + ch + sha256_round_k[j] + w[j]
			t2 := S0 + maj
			t[7] = t[6]
			t[6] = t[5]
			t[5] = t[4]
			t[4] = t[3] + t1
			t[3] = t[2]
			t[2] = t[1]
			t[1] = t[0]
			t[0] = t1 + t2
		}
		for i := 0; i < 8; i++ {
			v[i] += t[i]
		}
	}
	for i := 0; i < 8; i++ {
		sha256_endian_reverse32(v[i], output[i*4:])
	}
}
