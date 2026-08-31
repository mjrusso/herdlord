package skill

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
)

//go:embed SKILL.md
var markdown string

func Markdown() string { return markdown }

func SHA256() string {
	sum := sha256.Sum256([]byte(markdown))
	return hex.EncodeToString(sum[:])
}
