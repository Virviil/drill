package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var nums []int
	__arg(a[0], &nums)
	var k int
	__arg(a[1], &k)
	return topKFrequent(nums, k)
}
