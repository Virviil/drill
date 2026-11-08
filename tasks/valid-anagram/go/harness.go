package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var s string
	__arg(a[0], &s)
	var t string
	__arg(a[1], &t)
	return isAnagram(s, t)
}
