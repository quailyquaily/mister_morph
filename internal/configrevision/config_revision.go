package configrevision

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
)

type Snapshot struct {
	Data     []byte
	Revision string
}

func Read(path string) (Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return Snapshot{}, err
		}
		return Snapshot{Revision: Hash(nil)}, nil
	}
	return Snapshot{Data: data, Revision: Hash(data)}, nil
}

func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
