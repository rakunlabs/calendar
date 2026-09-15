package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/rakunlabs/ada"
	"github.com/rakunlabs/calendar/pkg/ical"
	"github.com/rakunlabs/calendar/pkg/models"
)

// Resolve and dial the validated address itself to prevent DNS rebinding. Redirects
// use this transport too; no proxy or ambient credentials are sent to feed hosts.
func publicFeedAddress(ip net.IP) bool {
	shared := &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() &&
		!ip.IsLinkLocalUnicast() && !shared.Contains(ip)
}

func feedURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(strings.ToLower(raw), "webcal://") {
		raw = "https://" + raw[len("webcal://"):]
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return nil, errors.New("use an HTTP, HTTPS or webcal calendar URL without embedded credentials")
	}
	return u, nil
}

func newFeedClient() *http.Client {
	transport := &http.Transport{
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, err
			}
			if len(ips) == 0 {
				return nil, errors.New("calendar host has no addresses")
			}
			for _, ip := range ips {
				if !publicFeedAddress(ip.IP) {
					return nil, errors.New("calendar URL must resolve to a public address")
				}
			}
			var dialer net.Dialer
			for _, ip := range ips {
				conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if dialErr == nil {
					return conn, nil
				}
				err = dialErr
			}
			return nil, err
		},
	}
	return &http.Client{Transport: transport, Timeout: 20 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many calendar redirects")
			}
			_, err := feedURL(req.URL.String())
			return err
		},
	}
}

// SubscriptionOccurrences fetches a read-only feed without importing its events.
// URLs are sent in a POST body because calendar links may contain private tokens.
func (h *HTTP) SubscriptionOccurrences(c *ada.Context) error {
	client := newFeedClient()
	defer client.CloseIdleConnections()
	return subscriptionOccurrences(c, client)
}

func subscriptionOccurrences(c *ada.Context, client *http.Client) error {
	var input struct {
		URL  string    `json:"url"`
		From time.Time `json:"from"`
		To   time.Time `json:"to"`
	}
	if err := json.NewDecoder(io.LimitReader(c.Request.Body, 16384)).Decode(&input); err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, "invalid subscription request")
	}
	u, err := feedURL(input.URL)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	if input.From.IsZero() || !input.To.After(input.From) || input.To.Sub(input.From) > 400*24*time.Hour {
		return ada.NewHTTPError(http.StatusBadRequest, "select a date range of at most 400 days")
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadRequest, "invalid calendar URL")
	}
	req.Header.Set("Accept", "text/calendar")
	response, err := client.Do(req)
	if err != nil {
		return ada.NewHTTPError(http.StatusBadGateway, "Could not fetch calendar. Check that the URL is publicly reachable.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ada.NewHTTPError(http.StatusBadGateway, "Calendar provider did not return a successful response.")
	}
	const maxBytes = 9 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
	if err != nil || len(data) > maxBytes {
		return ada.NewHTTPError(http.StatusBadGateway, "Calendar could not be read or exceeds 9 MiB.")
	}
	if !bytes.HasPrefix(bytes.TrimSpace(data), []byte("BEGIN:VCALENDAR")) {
		return ada.NewHTTPError(http.StatusUnprocessableEntity, "URL must return an ICS calendar, not a sharing web page.")
	}
	events, err := ical.ParseICS(bytes.NewReader(data), time.UTC)
	if err != nil {
		return ada.NewHTTPError(http.StatusUnprocessableEntity, "Calendar contains invalid or unsupported ICS data.")
	}
	result := []models.Event{}
	for _, event := range events {
		occurrences, err := ical.Occurrences(ctx, event, input.From, input.To)
		if err != nil {
			return ada.NewHTTPError(http.StatusUnprocessableEntity, "Calendar recurrence could not be expanded. Try a smaller date range.")
		}
		result = append(result, occurrences...)
		if len(result) > 20000 {
			return ada.NewHTTPError(http.StatusUnprocessableEntity, "Too many occurrences. Select a smaller date range.")
		}
	}
	slices.SortFunc(result, func(a, b models.Event) int { return a.DateFrom.Compare(b.DateFrom.Time) })
	c.Response.Header().Set("Cache-Control", "no-store")
	return c.SendJSON(Response[[]models.Event]{Payload: result})
}
