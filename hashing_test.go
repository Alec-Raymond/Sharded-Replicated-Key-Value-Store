package main

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func testfindShardN(t *testing.T, shardCount int, keys int) {
	shards := make(map[string][]string)
	res := make(map[string]int)
	for i := 0; i < shardCount; i++ {
		key := fmt.Sprintf("s%d", i)
		shards[key] = []string{}
		res[key] = 0
	}

	for i := 0; i < keys; i++ {
		res[findShard(fmt.Sprintf("key%d", i), shards)]++
	}

	fmt.Println("Result:", res)

	equalShare := float64(keys) / float64(len(shards))
	for _, v := range res {
		assert.Greater(t, float64(v), equalShare*.75)
		assert.Less(t, float64(v), equalShare*1.25)
	}

}

func Test_find2Shards600Keys(t *testing.T) {
	testfindShardN(t, 2, 600)
}

func Test_find3Shards600Keys(t *testing.T) {
	testfindShardN(t, 3, 600)
}
