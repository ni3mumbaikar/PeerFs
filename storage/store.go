package storage

import (
	"crypto/sha1"
	"encoding/hex"
	"path/filepath"
)

type PathKey struct {
	Pathname string
	Filename string
}

func (p PathKey) FullPath() string {
	return filepath.Join(p.Pathname, p.Filename)
}

func CASPath(realfilepath string, filename string) PathKey {

	hash := sha1.Sum([]byte(filename))
	hashedFilePath := hex.EncodeToString(hash[:])
	firstFolderPath := hashedFilePath[0:2]
	secondFolderPath := hashedFilePath[2:4]
	lastFolderPath := hashedFilePath[4:]

	casfilepath := filepath.Join(realfilepath, firstFolderPath, secondFolderPath)

	currentFilePathKey := PathKey{
		Pathname: casfilepath,
		Filename: lastFolderPath,
	}

	return currentFilePathKey
}
