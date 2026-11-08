package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var height []int
	__arg(a[0], &height)
	return maxArea(height)
}
