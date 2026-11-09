package hub

import (
	"sort"
	"sync"
	"time"
)

// Presence tracks who is connected and where they are.
type Presence struct {
	mu   sync.Mutex
	next int
	m    map[int]*Online
}

// Online is one live session.
type Online struct {
	Nick  string
	Door  string // "" = main menu
	Since time.Time
}

// NewPresence creates an empty registry.
func NewPresence() *Presence { return &Presence{m: map[int]*Online{}} }

// Join registers a session and returns its handle.
func (p *Presence) Join(nick string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.next++
	p.m[p.next] = &Online{Nick: nick, Since: time.Now()}
	return p.next
}

// Move records which door a session is in.
func (p *Presence) Move(id int, door string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if o, ok := p.m[id]; ok {
		o.Door = door
	}
}

// Leave removes a session.
func (p *Presence) Leave(id int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.m, id)
}

// List returns live sessions, one per nick (the most recent), sorted by nick.
func (p *Presence) List() []Online {
	p.mu.Lock()
	defer p.mu.Unlock()
	by := map[string]Online{}
	for _, o := range p.m {
		if prev, ok := by[o.Nick]; !ok || o.Since.After(prev.Since) {
			by[o.Nick] = *o
		}
	}
	out := make([]Online, 0, len(by))
	for _, o := range by {
		out = append(out, o)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Nick < out[j].Nick })
	return out
}
