package main

import (
	"crypto/md5"
	"slices"
)

func hash(key string) int {
	byteKey := ([]byte)(key)
	// The hashed checksum is 16 bytes.
	hashedKey := md5.Sum(byteKey)
	// To support a capacity (potential number of shards) of 256, we want 8 bits (1 byte).
	return (int)(hashedKey[15])
}

func findShard(key string, shardNames []string) string {
	var left, right, mid int
	right = len(shardNames) - 1

	keyHash := hash(key)

	// TO-DO: Move out to shard initialization/modification
	slices.SortFunc(shardNames, func(a, b string) int {
		comparison := hash(a) < hash(b)
		if comparison {
			return -1
		}
		return 1
	})

	for left < right {
		mid = left + (right-left)/2

		if keyHash > hash(shardNames[mid]) {
			left = mid + 1
		} else if keyHash < hash(shardNames[mid]) {
			right = mid - 1
		} else {
			return shardNames[mid]
		}
	}

	if left < len(shardNames)-1 {
		return shardNames[left+1]
	}

	return shardNames[0]
}
