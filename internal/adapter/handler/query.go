package handler

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/rakunlabs/query"
)

// Literal filters bypass the query language's comma lists and logical syntax.
// Keep the same expression representation used by the validated query endpoints.
func literalQuery(values url.Values, fields ...string) (*query.Query, error) {
	q := query.New()
	q.Values = make(map[string][]*query.ExpressionCmp)
	for key, values := range values {
		field := strings.TrimSuffix(key, "[eq]")
		allowed := false
		for _, candidate := range fields {
			allowed = allowed || field == candidate
		}
		if !allowed || len(values) != 1 || strings.TrimSpace(values[0]) == "" || q.Has(field) {
			return nil, fmt.Errorf("%s must be a single nonempty equality filter", key)
		}
		expr := query.NewExpressionCmp(query.OperatorEq, field, values[0])
		q.Values[field] = []*query.ExpressionCmp{expr}
		q.Where = append(q.Where, expr)
	}
	return q, nil
}

func parseQuery(raw string, validator *query.Validator, opts ...query.OptionQuery) (*query.Query, error) {
	opts = append(opts, query.WithKeyAlias(
		"limit", "_limit",
		"offset", "_offset",
		"sort", "_sort",
		"fields", "_fields",
	))
	return query.ParseWithValidator(raw, validator, opts...)
}
