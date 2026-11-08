// Command gentests writes tasks/*/tests.json: hand-written public cases,
// edge cases and large random cases whose answers come from reference
// solutions below. Spoilers ahead: reference solutions live in this file.
//
//	go run ./tools/gentests            # all tasks
//	go run ./tools/gentests two-sum    # one task
package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type test struct {
	Args   []any `json:"args"`
	Want   any   `json:"want"`
	Public bool  `json:"public"`
}

type gen func(r *rand.Rand) []test

var tasks = map[string]gen{
	"contains-duplicate":      containsDuplicate,
	"valid-anagram":           validAnagram,
	"two-sum":                 twoSum,
	"top-k-frequent":          topKFrequent,
	"product-except-self":     productExceptSelf,
	"longest-consecutive":     longestConsecutive,
	"valid-palindrome":        validPalindrome,
	"container-most-water":    containerMostWater,
	"best-time-stock":         bestTimeStock,
	"longest-substring-norep": longestSubstring,
}

func main() {
	root := "tasks"
	names := os.Args[1:]
	if len(names) == 0 {
		for k := range tasks {
			names = append(names, k)
		}
		sort.Strings(names)
	}
	for _, name := range names {
		g, ok := tasks[name]
		if !ok {
			fmt.Fprintln(os.Stderr, "unknown task", name)
			os.Exit(1)
		}
		r := rand.New(rand.NewSource(int64(len(name)) * 7919))
		ts := g(r)
		b, err := json.Marshal(ts)
		if err != nil {
			panic(err)
		}
		// one test per line keeps diffs readable
		s := strings.ReplaceAll(string(b), `},{"args"`, "},\n{\"args\"")
		path := filepath.Join(root, name, "tests.json")
		if err := os.WriteFile(path, []byte(s+"\n"), 0o644); err != nil {
			panic(err)
		}
		fmt.Printf("%-26s %3d tests\n", name, len(ts))
	}
}

func pub(args ...any) func(want any) test {
	return func(want any) test { return test{Args: args, Want: want, Public: true} }
}

func hid(want any, args ...any) test { return test{Args: args, Want: want} }

func randInts(r *rand.Rand, n, lo, hi int) []int {
	a := make([]int, n)
	for i := range a {
		a[i] = lo + r.Intn(hi-lo+1)
	}
	return a
}

// ---- contains-duplicate ----

func refContainsDuplicate(a []int) bool {
	seen := map[int]bool{}
	for _, v := range a {
		if seen[v] {
			return true
		}
		seen[v] = true
	}
	return false
}

func containsDuplicate(r *rand.Rand) []test {
	ts := []test{
		pub([]int{1, 2, 3, 1})(true),
		pub([]int{1, 2, 3, 4})(false),
		pub([]int{1, 1, 1, 3, 3, 4, 3, 2, 4, 2})(true),
		hid(false, []int{7}),
		hid(true, []int{-5, -5}),
	}
	perm := r.Perm(100000)
	ts = append(ts, hid(false, perm))
	dup := append([]int(nil), perm...)
	dup[99999] = dup[0]
	ts = append(ts, hid(true, dup))
	for i := 0; i < 5; i++ {
		a := randInts(r, 1+r.Intn(50), -20, 60)
		ts = append(ts, hid(refContainsDuplicate(a), a))
	}
	return ts
}

// ---- valid-anagram ----

func refAnagram(s, t string) bool {
	if len(s) != len(t) {
		return false
	}
	var c [26]int
	for i := range s {
		c[s[i]-'a']++
		c[t[i]-'a']--
	}
	return c == [26]int{}
}

func randWord(r *rand.Rand, n int, alpha string) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = alpha[r.Intn(len(alpha))]
	}
	return string(b)
}

func shuffle(r *rand.Rand, s string) string {
	b := []byte(s)
	r.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
	return string(b)
}

func validAnagram(r *rand.Rand) []test {
	ts := []test{
		pub("anagram", "nagaram")(true),
		pub("rat", "car")(false),
		pub("ab", "a")(false),
		hid(true, "a", "a"),
		hid(false, "aacc", "ccac"),
	}
	big := randWord(r, 50000, "abcdefghijklmnopqrstuvwxyz")
	ts = append(ts, hid(true, big, shuffle(r, big)))
	other := []byte(shuffle(r, big))
	other[0] = 'a' + (other[0]-'a'+1)%26
	ts = append(ts, hid(refAnagram(big, string(other)), big, string(other)))
	for i := 0; i < 6; i++ {
		s := randWord(r, 1+r.Intn(12), "abc")
		t := shuffle(r, s)
		if i%2 == 1 {
			t = randWord(r, len(s), "abc")
		}
		ts = append(ts, hid(refAnagram(s, t), s, t))
	}
	return ts
}

// ---- two-sum ----

func twoSum(r *rand.Rand) []test {
	ts := []test{
		pub([]int{2, 7, 11, 15}, 9)([]int{0, 1}),
		pub([]int{3, 2, 4}, 6)([]int{1, 2}),
		pub([]int{3, 3}, 6)([]int{0, 1}),
		hid([]int{0, 4}, []int{-1, -20, -30, -40, 5}, 4),
	}
	// big: distinct even numbers, the only odd pair is the answer
	for _, n := range []int{1000, 100000} {
		a := make([]int, n)
		for i := range a {
			a[i] = 2 * (i + 1)
		}
		r.Shuffle(n, func(i, j int) { a[i], a[j] = a[j], a[i] })
		// answer near the end so a brute-force double loop does ~n²/2 work
		i, j := n-10+r.Intn(5), n-5+r.Intn(5)
		a[i], a[j] = 1_000_000_001, -999_999_998
		ts = append(ts, hid([]int{i, j}, a, 3))
	}
	return ts
}

// ---- top-k-frequent ----

func topKFrequent(r *rand.Rand) []test {
	ts := []test{
		pub([]int{1, 1, 1, 2, 2, 3}, 2)([]int{1, 2}),
		pub([]int{1}, 1)([]int{1}),
		pub([]int{4, 4, 4, 5, 5, 6, 6, 6, 6}, 1)([]int{6}),
		hid([]int{-1, 2}, []int{-1, -1, 2, 2, 3}, 2),
	}
	// big: value v appears freq[v] times, all frequencies distinct
	vals := r.Perm(400)
	var a []int
	for i, v := range vals {
		for c := 0; c <= i; c++ {
			a = append(a, v-200)
		}
	}
	r.Shuffle(len(a), func(i, j int) { a[i], a[j] = a[j], a[i] })
	k := 10
	var want []int
	for i := len(vals) - 1; i >= len(vals)-k; i-- {
		want = append(want, vals[i]-200)
	}
	ts = append(ts, hid(want, a, k))
	return ts
}

// ---- product-except-self ----

func refProduct(a []int) []int {
	n := len(a)
	out := make([]int, n)
	p := 1
	for i := 0; i < n; i++ {
		out[i] = p
		p *= a[i]
	}
	p = 1
	for i := n - 1; i >= 0; i-- {
		out[i] *= p
		p *= a[i]
	}
	return out
}

func productExceptSelf(r *rand.Rand) []test {
	ts := []test{
		pub([]int{1, 2, 3, 4})([]int{24, 12, 8, 6}),
		pub([]int{-1, 1, 0, -3, 3})([]int{0, 0, 9, 0, 0}),
		pub([]int{2, 3})([]int{3, 2}),
		hid([]int{0, 0}, []int{0, 0}),
		hid([]int{0, 0, 6}, []int{2, 3, 0}),
	}
	// big: mostly ±1 so products stay small
	a := make([]int, 100000)
	for i := range a {
		a[i] = 1
		if r.Intn(2) == 0 {
			a[i] = -1
		}
	}
	for i := 0; i < 20; i++ {
		a[r.Intn(len(a))] = 2
	}
	ts = append(ts, hid(refProduct(a), a))
	for i := 0; i < 4; i++ {
		b := randInts(r, 2+r.Intn(8), -4, 4)
		ts = append(ts, hid(refProduct(b), b))
	}
	return ts
}

// ---- longest-consecutive ----

func refConsecutive(a []int) int {
	set := map[int]bool{}
	for _, v := range a {
		set[v] = true
	}
	best := 0
	for v := range set {
		if set[v-1] {
			continue
		}
		n := 1
		for set[v+n] {
			n++
		}
		if n > best {
			best = n
		}
	}
	return best
}

func longestConsecutive(r *rand.Rand) []test {
	ts := []test{
		pub([]int{100, 4, 200, 1, 3, 2})(4),
		pub([]int{0, 3, 7, 2, 5, 8, 4, 6, 0, 1})(9),
		pub([]int{1, 0, 1, 2})(3),
		hid(0, []int{}),
		hid(1, []int{-7}),
	}
	a := randInts(r, 100000, -1_000_000_000, 1_000_000_000)
	for i := 0; i < 5000; i++ {
		a[r.Intn(len(a))] = 500 + i
	}
	ts = append(ts, hid(refConsecutive(a), a))
	b := r.Perm(100000)
	ts = append(ts, hid(100000, b))
	return ts
}

// ---- valid-palindrome ----

func refPalindrome(s string) bool {
	var b []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b = append(b, c)
		}
	}
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		if b[i] != b[j] {
			return false
		}
	}
	return true
}

func validPalindrome(r *rand.Rand) []test {
	ts := []test{
		pub("A man, a plan, a canal: Panama")(true),
		pub("race a car")(false),
		pub(" ")(true),
		hid(false, "0P"),
		hid(true, "a."),
		hid(true, ".,"),
		hid(true, "ab_a"), // "_" is dropped: "aba"
	}
	half := randWord(r, 100000, "abcAB12 ,.")
	rev := []byte(half)
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	s := half + string(rev)
	ts = append(ts, hid(refPalindrome(s), s))
	return ts
}

// ---- container-most-water ----

func refArea(h []int) int {
	best, i, j := 0, 0, len(h)-1
	for i < j {
		w := j - i
		m := h[i]
		if h[j] < m {
			m = h[j]
		}
		if w*m > best {
			best = w * m
		}
		if h[i] < h[j] {
			i++
		} else {
			j--
		}
	}
	return best
}

func containerMostWater(r *rand.Rand) []test {
	ts := []test{
		pub([]int{1, 8, 6, 2, 5, 4, 8, 3, 7})(49),
		pub([]int{1, 1})(1),
		pub([]int{4, 3, 2, 1, 4})(16),
		hid(2, []int{1, 2, 1}),
	}
	a := randInts(r, 100000, 0, 10000)
	ts = append(ts, hid(refArea(a), a))
	for i := 0; i < 4; i++ {
		b := randInts(r, 2+r.Intn(15), 0, 20)
		ts = append(ts, hid(refArea(b), b))
	}
	return ts
}

// ---- best-time-stock ----

func refProfit(p []int) int {
	best, lo := 0, p[0]
	for _, v := range p {
		if v < lo {
			lo = v
		}
		if v-lo > best {
			best = v - lo
		}
	}
	return best
}

func bestTimeStock(r *rand.Rand) []test {
	ts := []test{
		pub([]int{7, 1, 5, 3, 6, 4})(5),
		pub([]int{7, 6, 4, 3, 1})(0),
		pub([]int{2, 4, 1})(2),
		hid(0, []int{5}),
	}
	a := randInts(r, 100000, 0, 10000)
	ts = append(ts, hid(refProfit(a), a))
	for i := 0; i < 4; i++ {
		b := randInts(r, 1+r.Intn(12), 0, 30)
		ts = append(ts, hid(refProfit(b), b))
	}
	return ts
}

// ---- longest-substring-norep ----

func refLongest(s string) int {
	last := map[byte]int{}
	best, start := 0, 0
	for i := 0; i < len(s); i++ {
		if j, ok := last[s[i]]; ok && j >= start {
			start = j + 1
		}
		last[s[i]] = i
		if i-start+1 > best {
			best = i - start + 1
		}
	}
	return best
}

func longestSubstring(r *rand.Rand) []test {
	ts := []test{
		pub("abcabcbb")(3),
		pub("bbbbb")(1),
		pub("pwwkew")(3),
		hid(0, ""),
		hid(1, " "),
		hid(3, "dvdf"),
		hid(2, "abba"),
	}
	alpha := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 !?"
	s := randWord(r, 100000, alpha)
	ts = append(ts, hid(refLongest(s), s))
	return ts
}
