package hub

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func dial(t *testing.T, srv *httptest.Server) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	return conn
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met in time")
}

func TestBroadcastReachesAllClientsAndCleansUp(t *testing.T) {
	h := New(func(*http.Request) bool { return true })
	srv := httptest.NewServer(http.HandlerFunc(h.ServeWS))
	defer srv.Close()

	c1, c2 := dial(t, srv), dial(t, srv)
	waitFor(t, func() bool { return h.ClientCount() == 2 })

	if n := h.Broadcast([]byte(`{"hello":"world"}`)); n != 2 {
		t.Fatalf("Broadcast delivered to %d clients, want 2", n)
	}
	for i, c := range []*websocket.Conn{c1, c2} {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, msg, err := c.ReadMessage()
		if err != nil || string(msg) != `{"hello":"world"}` {
			t.Fatalf("client %d got %q, err %v", i, msg, err)
		}
	}

	c1.Close()
	waitFor(t, func() bool { return h.ClientCount() == 1 })

	h.Close()
	waitFor(t, func() bool { return h.ClientCount() == 0 })
	c2.Close()
}
