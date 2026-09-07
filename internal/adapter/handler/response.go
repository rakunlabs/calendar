package handler

import (
	"errors"
	"net/http"

	"github.com/rakunlabs/ada"
)

// Response is the calendar API envelope, including a payload even when it is nil.
type Response[T any] struct {
	Message *Message `json:"message,omitempty"`
	Meta    *Meta    `json:"meta,omitempty"`
	Payload T        `json:"payload"`
}

type ResponseMessage struct {
	Message *Message `json:"message,omitempty"`
}

type Message struct {
	Text   string         `json:"text,omitempty"`
	Params map[string]any `json:"params,omitempty"`
	Err    string         `json:"error,omitempty"`
}

type Meta struct {
	TotalItemCount uint64 `json:"total_item_count,omitempty"`
	Limit          uint64 `json:"limit,omitempty"`
	Offset         uint64 `json:"offset,omitempty"`
}

// Error-valued messages historically kept the generic text, even for a 400.
func responseError(code int, err error) *ada.HTTPError {
	return &ada.HTTPError{Code: code, Message: http.StatusText(http.StatusInternalServerError), Err: err}
}

func HTTPErrorHandler(c *ada.Context, err error) {
	if c.Committed() {
		return
	}

	code := http.StatusInternalServerError
	message := &Message{Text: http.StatusText(code)}
	if he, ok := errors.AsType[*ada.HTTPError](err); ok {
		code = he.Code
		message.Text = he.Message
		if he.Err != nil {
			message.Err = he.Err.Error()
		}
	}

	if c.Request.Method == http.MethodHead {
		c.Response.WriteHeader(code)
		return
	}

	_ = c.SetStatus(code).SendJSON(ResponseMessage{Message: message})
}
