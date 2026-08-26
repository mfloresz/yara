package api

import (
	"encoding/json"
	"strconv"

	"github.com/pocketbase/pocketbase/core"
)

// v1Envelope: canonical REST envelope for v1 responses. Single resources use
// {data: <resource>}. Collections use {data, meta, links} with optional cursor
// pagination; offset pagination (page/per_page) is also supported and emitted
// in meta. Legacy routes are kept verbatim — only /api/v1/* paths use this shape.
type v1Envelope struct {
	Data  any         `json:"data"`
	Meta  *v1Meta     `json:"meta,omitempty"`
	Links *v1Links    `json:"links,omitempty"`
	Error *v1ErrorObj `json:"error,omitempty"`
}

type v1Meta struct {
	Total    int  `json:"total"`
	Page     int  `json:"page,omitempty"`
	PerPage  int  `json:"per_page,omitempty"`
	Limit    int  `json:"limit,omitempty"`
	Offset   int  `json:"offset,omitempty"`
	HasMore  bool `json:"has_more"`
	NextPage *int `json:"next_page,omitempty"`
}

type v1Links struct {
	Self  string `json:"self"`
	Next  string `json:"next,omitempty"`
	Prev  string `json:"prev,omitempty"`
	First string `json:"first,omitempty"`
	Last  string `json:"last,omitempty"`
}

type v1ErrorObj struct {
	Code    string           `json:"code"`
	Message string           `json:"message"`
	Details []v1ErrorDetail  `json:"details,omitempty"`
}

type v1ErrorDetail struct {
	Field   string `json:"field,omitempty"`
	Message string `json:"message"`
	Code    string `json:"code,omitempty"`
}

// parsePagination reads ?page&per_page and ?cursor&limit, clamping to safe bounds.
// Page/per_page is the offset-based pagination the legacy API already exposes.
// Cursor support is in place for future use; current store layer still uses
// limit/offset under the hood, so we treat cursor as an offset token for now.
func parsePagination(q map[string][]string) (page, perPage, limit, offset int) {
	if v := firstQuery(q, "per_page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			perPage = n
			if perPage > 200 {
				perPage = 200
			}
		}
	}
	if perPage == 0 {
		perPage = 50
	}
	if v := firstQuery(q, "page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}
	if page == 0 {
		page = 1
	}
	offset = (page - 1) * perPage
	limit = perPage
	if v := firstQuery(q, "limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
			if limit > 200 {
				limit = 200
			}
		}
	}
	if v := firstQuery(q, "offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return page, perPage, limit, offset
}

func firstQuery(q map[string][]string, key string) string {
	if v, ok := q[key]; ok && len(v) > 0 {
		return v[0]
	}
	return ""
}

// v1Respond: write a v1 envelope to the request. status is the HTTP status
// code; the body is {data, meta, links} when meta/links are provided, or just
// {data} otherwise. Errors use writeV1Error instead.
//
// This bypasses PocketBase's e.JSON because the built-in e.JSON runs the
// request's ?fields= query through picker.Pick on the top-level object, which
// would strip the v1 envelope's data/meta/links and emit an empty object.
// Writing directly to e.Response keeps the envelope shape stable.
func v1Respond(e *core.RequestEvent, status int, data any, meta *v1Meta, links *v1Links) error {
	body := v1Envelope{Data: data}
	if meta != nil {
		body.Meta = meta
	}
	if links != nil {
		body.Links = links
	}
	e.Response.Header().Set("Content-Type", "application/json")
	e.Response.WriteHeader(status)
	return json.NewEncoder(e.Response).Encode(body)
}

func v1RespondList(e *core.RequestEvent, status int, data any, page, perPage, total int, hasMore bool, self string) error {
	meta := &v1Meta{
		Total:   total,
		Page:    page,
		PerPage: perPage,
		Limit:   perPage,
		Offset:  (page - 1) * perPage,
		HasMore: hasMore,
	}
	links := &v1Links{Self: self}
	if hasMore {
		links.Next = addQueryParam(self, "page", strconv.Itoa(page+1))
	}
	if page > 1 {
		links.Prev = addQueryParam(self, "page", strconv.Itoa(page-1))
	}
	return v1Respond(e, status, data, meta, links)
}

func addQueryParam(rawURL, key, value string) string {
	if rawURL == "" {
		return ""
	}
	sep := "?"
	for i := 0; i < len(rawURL); i++ {
		if rawURL[i] == '?' {
			sep = "&"
			break
		}
	}
	return rawURL + sep + key + "=" + value
}

// writeV1Error: standard error envelope. Content-Type is application/problem+json
// per RFC 7807 (with a json alias for clients that don't honour problem+json
// content negotiation yet). Map PocketBase errors through this helper.
//
// Bypasses e.JSON for the same reason as v1Respond: built-in e.JSON would
// run picker.Pick on the top-level object and strip the error envelope.
func writeV1Error(e *core.RequestEvent, status int, code, message string, details ...v1ErrorDetail) error {
	e.Response.Header().Set("Content-Type", "application/problem+json")
	e.Response.WriteHeader(status)
	return json.NewEncoder(e.Response).Encode(v1Envelope{Error: &v1ErrorObj{Code: code, Message: message, Details: details}})
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// isV1Request reports whether the current request targets a /api/v1/* route.
// Handlers branch on this to pick the canonical envelope shape; legacy
// /api/db/* /api/user/* /api/epubs/* paths return the original {items,hasMore}
// or bare-array shape so the existing frontend keeps working unchanged.
func isV1Request(e *core.RequestEvent) bool {
	return hasPrefix(e.Request.URL.Path, "/api/v1/")
}
