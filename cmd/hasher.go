package cmd

import (
	"crypto/md5"
	"encoding/hex"

	"github.com/minio/sha256-simd"
)

// getSHA256Hash returns SHA-256 hash in hex encoding of given data.
func getSHA256Hash(data []byte) string {
	return hex.EncodeToString(getSHA256Sum(data))
}

// getSHA256Hash returns SHA-256 sum of given data.
func getSHA256Sum(data []byte) []byte {
	hash := sha256.New()
	hash.Write(data)
	return hash.Sum(nil)
}

// getMD5Sum returns MD5 sum of given data.
func getMD5Sum(data []byte) []byte {
	hash := md5.New()
	hash.Write(data)
	return hash.Sum(nil)
}

// getMD5Hash returns MD5 hash in hex encoding of given data.
func getMD5Hash(data []byte) string {
	return hex.EncodeToString(getMD5Sum(data))
}
