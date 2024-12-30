package pg

import (
	"github.com/alaisi/syscalltodo/crypto"
	"github.com/alaisi/syscalltodo/str"
)

func scramBuildClientFirst() ([]byte, error) {
	nonce := make([]byte, 32)
	if err := crypto.Rand(nonce); err != nil {
		return nil, err
	}
	return []byte("n,,n=*,r=" + str.EncodeB64(nonce)), nil
}

func scramHashPassword(password string, serverFirst []byte) []byte {
	challenge := parseFields(serverFirst)
	salt := str.DecodeB64(challenge["s"])
	iterations := str.Atoi(challenge["i"])
	return crypto.Pbkdf2HmacSha256([]byte(password), salt, iterations)
}

func scramBuildClientFinal(
	clientFirst []byte,
	saltedPassword []byte,
	serverFirst []byte,
) []byte {
	challenge := parseFields(serverFirst)
	nonce := parseFields(clientFirst[3:])["r"]
	if nonce != challenge["r"][0:len(nonce)] {
		return nil
	}
	clientKey := crypto.HmacSha256(saltedPassword, []byte("Client Key"))
	storedKey := crypto.Sha256(clientKey)
	clientFinalWithoutProof := "c=biws,r=" + challenge["r"]
	authMessage := string(clientFirst[3:]) + "," +
		string(serverFirst) + "," + clientFinalWithoutProof
	clientSignature := crypto.HmacSha256(storedKey, []byte(authMessage))
	for i := 0; i < len(clientKey); i++ {
		clientKey[i] ^= clientSignature[i]
	}
	return []byte(clientFinalWithoutProof + ",p=" + str.EncodeB64(clientKey))
}

func scramAuthenticateServer(
	clientFirst []byte,
	serverFirst []byte,
	saltedPassword []byte,
	clientFinal []byte,
	serverFinal []byte,
) bool {
	clientFinalStr := string(clientFinal)
	authMessage := string(clientFirst)[3:] + "," +
		string(serverFirst) + "," +
		clientFinalStr[0:str.IndexOfString(clientFinalStr, ",p=")]
	serverKey := crypto.HmacSha256(saltedPassword, []byte("Server Key"))
	serverSignature := crypto.HmacSha256(serverKey, []byte(authMessage))
	verifier := str.DecodeB64(parseFields(serverFinal)["v"])
	if len(serverSignature) != len(verifier) {
		return false
	}
	for i, b := range serverSignature {
		if verifier[i] != b {
			return false
		}
	}
	return true
}

func parseFields(b []byte) map[string]string {
	fields := make(map[string]string)
	for _, kv := range str.Split(string(b), ',') {
		eq := str.IndexOf(kv, '=')
		fields[kv[0:eq]] = kv[eq+1:]
	}
	return fields
}
