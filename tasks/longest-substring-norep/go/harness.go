package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var s string
	__arg(a[0], &s)
	return lengthOfLongestSubstring(s)
}
