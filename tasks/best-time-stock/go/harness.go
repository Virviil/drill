package main

import "encoding/json"

func __call(a []json.RawMessage) any {
	var prices []int
	__arg(a[0], &prices)
	return maxProfit(prices)
}
