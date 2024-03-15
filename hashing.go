package main

import (
	"crypto/sha1"
	"fmt"
	"slices"
)

func hash(key string) int {
	byteKey := ([]byte)(key)
	// The hashed checksum is 16 bytes.
	hashedKey := sha1.Sum(byteKey)
	// To support a capacity (potential number of shards) of 256, we want 8 bits (1 byte).
	return (int)(hashedKey[19]) % 128
}

func findShard(key string, shards map[string][]string) string {
	shardNames := make([]string, 0)
	for s := range shards {
		shardNames = append(shardNames, s)
	}
	// var left, right, mid int
	// right = len(shardNames) - 1

	keyHash := hash(key)

	// TO-DO: Move out to shard initialization/modification
	slices.SortFunc(shardNames, func(a, b string) int {
		comparison := hash(a) < hash(b)
		if comparison {
			return -1
		}
		return 1
	})

	var shardHashes []int

	for _, shardName := range shardNames {
		shardHashes = append(shardHashes, hash(shardName))
	}

	// zap.L().Debug("Shard Name Hashes:", zap.Any("shardHashes", shardHashes))
	fmt.Println("Shard Name Hashes:", shardHashes)

	for _, shardName := range shardNames {
		if hash(shardName) > keyHash {
			fmt.Printf("key %s:%d went to shard %d\n", key, keyHash, hash(shardName))
			return shardName
		}
	}

	return shardNames[0]

}
