package client

import (
	"fmt"

	"github.com/rakunlabs/ok"
)

type Calendar struct {
	client *ok.Client
}

func New(opts ...ok.OptionClientFn) (*Calendar, error) {
	client, err := ok.New(opts...)
	if err != nil {
		return nil, fmt.Errorf("provider client creation error=%w", err)
	}

	return &Calendar{
		client: client,
	}, nil
}
