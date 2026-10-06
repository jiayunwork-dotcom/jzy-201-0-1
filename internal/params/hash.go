package params

import (
	"crypto/sha256"
	"encoding/hex"
)

func stableSHA256(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
