package tests

import (
	"testing"
	"time"

	"builder/backend/internal/split"
)

func recv(t *testing.T, ch <-chan split.Event) (split.Event, bool) {
	t.Helper()
	select {
	case ev, ok := <-ch:
		return ev, ok
	case <-time.After(time.Second):
		t.Fatal("таймаут ожидания события")
		return split.Event{}, false
	}
}

func TestHubPublishSubscribe(t *testing.T) {
	h := split.NewHub()
	a, cancelA := h.Subscribe(1)
	b, _ := h.Subscribe(1)
	other, _ := h.Subscribe(2)

	h.Publish(1, split.NewEvent("status_changed", map[string]string{"status": "Open"}))
	for _, ch := range []<-chan split.Event{a, b} {
		ev, ok := recv(t, ch)
		if !ok || ev.Type != "status_changed" || string(ev.Data) != `{"status":"Open"}` {
			t.Fatalf("event = %+v ok=%v", ev, ok)
		}
	}
	select {
	case ev := <-other:
		t.Fatalf("чужое пространство получило %+v", ev)
	default:
	}

	cancelA()
	cancelA() // идемпотентно
	if _, ok := <-a; ok {
		t.Fatal("канал после отписки должен быть закрыт")
	}
	h.Publish(1, split.NewEvent("x", nil)) // не паникует на закрытом
}

func TestHubSlowSubscriberDoesNotBlock(t *testing.T) {
	h := split.NewHub()
	ch, _ := h.Subscribe(1)
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			h.Publish(1, split.NewEvent("e", i))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish заблокировался на медленном подписчике")
	}
	if len(ch) == 0 {
		t.Fatal("подписчик не получил ни одного события")
	}
}

func TestHubCloseSplitAndClose(t *testing.T) {
	h := split.NewHub()
	a, cancelA := h.Subscribe(1)
	b, _ := h.Subscribe(2)

	h.CloseSplit(1)
	if _, ok := recv(t, a); ok {
		t.Fatal("канал пространства 1 должен закрыться")
	}
	cancelA() // после CloseSplit не паникует

	h.Close()
	if _, ok := recv(t, b); ok {
		t.Fatal("канал пространства 2 должен закрыться")
	}
	late, _ := h.Subscribe(3)
	if _, ok := recv(t, late); ok {
		t.Fatal("подписка после Close должна сразу закрываться")
	}
}
