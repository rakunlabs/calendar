package handler

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/pkg/models"
	"github.com/stretchr/testify/require"
)

func TestFeedURL(t *testing.T) {
	u, err := feedURL(" webcal://example.com/calendar.ics?token=abc ")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/calendar.ics?token=abc", u.String())
	for _, raw := range []string{"file:///etc/passwd", "ftp://example.com/a", "https://user:pass@example.com/a", "not a url"} {
		_, err := feedURL(raw)
		require.Error(t, err, raw)
	}
}

func TestSubscriptionExpandsFeedWithoutImport(t *testing.T) {
	feed := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:remote-meeting\r\nSUMMARY:Remote meeting\r\nDTSTART:20260915T090000Z\r\nDTEND:20260915T100000Z\r\nRRULE:FREQ=DAILY;COUNT=3\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Authorization"))
		fmt.Fprint(w, feed)
	}))
	defer server.Close()
	body := fmt.Sprintf(`{"url":%q,"from":"2026-09-16T00:00:00Z","to":"2026-09-18T00:00:00Z"}`, server.URL)
	r := httptest.NewRequest(http.MethodPost, "/subscriptions/occurrences", strings.NewReader(body))
	w := httptest.NewRecorder()
	require.NoError(t, subscriptionOccurrences(ada.NewContext(w, r), server.Client()))
	var result Response[[]models.Event]
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Payload, 2)
	require.Equal(t, "Remote meeting", result.Payload[0].Name)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestFeedRejectsPrivateAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "::1", "fc00::1", "fe80::1", "::ffff:127.0.0.1", "0.0.0.0", "224.0.0.1"} {
		require.False(t, publicFeedAddress(net.ParseIP(raw)), raw)
	}
	require.True(t, publicFeedAddress(net.ParseIP("8.8.8.8")))
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	defer server.Close()
	client := newFeedClient()
	defer client.CloseIdleConnections()
	_, err := client.Get(server.URL)
	require.Error(t, err)
	require.False(t, called)
}

func TestSubscriptionValidation(t *testing.T) {
	h := &HTTP{}
	for _, body := range []string{
		`{}`, `{"url":"file:///tmp/test.ics"}`,
		`{"url":"https://example.com/test.ics","from":"2026-01-01T00:00:00Z","to":"2028-01-01T00:00:00Z"}`,
	} {
		r := httptest.NewRequest(http.MethodPost, "/subscriptions/occurrences", strings.NewReader(body))
		w := httptest.NewRecorder()
		c := ada.NewContext(w, r)
		err := h.SubscriptionOccurrences(c)
		require.Error(t, err)
		HTTPErrorHandler(c, err)
		require.Equal(t, http.StatusBadRequest, w.Code)
	}
}
