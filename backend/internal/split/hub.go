package split

import (
	"encoding/json"
	"sync"
)

const subscriberBuffer = 16

// Event — событие SSE-потока лобби.
type Event struct {
	Type string
	Data []byte // готовый JSON
}

func NewEvent(typ string, data any) Event {
	b, err := json.Marshal(data)
	if err != nil {
		b = []byte("{}")
	}
	return Event{Type: typ, Data: b}
}

// Hub — in-process рассылка событий подписчикам потока пространства.
// Redis и брокеры не нужны: сервис одноинстансный (см. CLAUDE.md).
type Hub struct {
	mu     sync.Mutex
	subs   map[int64]map[chan Event]struct{}
	closed bool
}

func NewHub() *Hub {
	return &Hub{subs: map[int64]map[chan Event]struct{}{}}
}

// Subscribe возвращает канал событий и функцию отписки (идемпотентна).
// После Close или CloseSplit канал закрывается.
func (h *Hub) Subscribe(splitID int64) (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		close(ch)
		return ch, func() {}
	}
	if h.subs[splitID] == nil {
		h.subs[splitID] = map[chan Event]struct{}{}
	}
	h.subs[splitID][ch] = struct{}{}
	return ch, func() { h.unsubscribe(splitID, ch) }
}

// Publish не блокируется: события несут полное состояние, поэтому для
// переполненного (медленного) подписчика пропуск не страшен.
func (h *Hub) Publish(splitID int64, ev Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[splitID] {
		select {
		case ch <- ev:
		default:
		}
	}
}

// CloseSplit завершает все потоки пространства (например, после удаления).
func (h *Hub) CloseSplit(splitID int64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.subs[splitID] {
		close(ch)
	}
	delete(h.subs, splitID)
}

// Close завершает все потоки; нужен для graceful shutdown.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	for id, set := range h.subs {
		for ch := range set {
			close(ch)
		}
		delete(h.subs, id)
	}
}

func (h *Hub) unsubscribe(splitID int64, ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[splitID][ch]; !ok {
		return // уже закрыт CloseSplit/Close
	}
	delete(h.subs[splitID], ch)
	if len(h.subs[splitID]) == 0 {
		delete(h.subs, splitID)
	}
	close(ch)
}
