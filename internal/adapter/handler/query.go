package handler

import "github.com/rakunlabs/query"

func parseQuery(raw string, validator *query.Validator, opts ...query.OptionQuery) (*query.Query, error) {
	opts = append(opts, query.WithKeyAlias(
		"limit", "_limit",
		"offset", "_offset",
		"sort", "_sort",
		"fields", "_fields",
	))
	return query.ParseWithValidator(raw, validator, opts...)
}
