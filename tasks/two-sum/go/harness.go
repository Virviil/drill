package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var nums []int
	__arg(a[0], &nums)
	var target int
	__arg(a[1], &target)
	return twoSum(nums, target)
}
