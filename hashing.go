package main

import (
	"crypto/sha1"
	"slices"
)

func hash(key string, numShards int) int {
	byteKey := ([]byte)(key)
	// The hashed checksum is 16 bytes.
	hashedKey := sha1.Sum(byteKey)
	// To support a capacity (potential number of shards) of 256, we want 8 bits (1 byte).
	return (int)(hashedKey[19]) % numShards
}

func findShard(key string, shards map[string][]string) string {
	shardNames := make([]string, 0)
	for s := range shards {
		shardNames = append(shardNames, s)
	}
	// var left, right, mid int
	// right = len(shardNames) - 1

	keyHash := hash(key, len(shards))
	slices.Sort(shardNames)

	// // // TO-DO: Move out to shard initialization/modification
	// slices.SortFunc(shardNames, func(a, b string) int {
	// 	comparison := hash(a, len(shards)) < hash(b, len(shards))
	// 	if comparison {
	// 		return -1
	// 	}
	// 	return 1
	// })

	// var shardHashes []int

	// for _, shardName := range shardNames {
	// 	shardHashes = append(shardHashes, hash(shardName))
	// }

	// // zap.L().Debug("Shard Name Hashes:", zap.Any("shardHashes", shardHashes))

	// for _, shardName := range shardNames {
	// 	if hash(shardName) > keyHash {
	// 		return shardName
	// 	}
	// }

	// return shardNames[0]

	return shardNames[keyHash]
}
