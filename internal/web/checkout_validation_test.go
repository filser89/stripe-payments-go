package web

import (
	"encoding/json"
	"errors"
	"github.com/filser89/stripe-payments-go/internal/payment"
	"github.com/filser89/stripe-payments-go/internal/testutil"
	"github.com/stretchr/testify/require"
	"io"
	"strings"
	"testing"
	"time"
)

func TestCheckoutValidation(t *testing.T) { // INP-001 INP-002 INP-003 INP-004 INP-005 INP-006 SEC-003
	for _, tc := range []struct {
		name, method, path, body, media, encoding string
		status                                    int
	}{
		{"description_one", "POST", "/api/orders", "{\"description\":\"a\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},                                                                                                                                                                                                                     // INP-001
		{"description_two_hundred", "POST", "/api/orders", "{\"description\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},      // INP-001
		{"description_empty", "POST", "/api/orders", "{\"description\":\"\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                                                                                                                                                                                                    // INP-001
		{"description_two_hundred_one", "POST", "/api/orders", "{\"description\":\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400}, // INP-001
		{"description_multibyte_200", "POST", "/api/orders", "{\"description\":\"\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},       // INP-001
		{"description_multibyte_201", "POST", "/api/orders", "{\"description\":\"\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\u00e9\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400}, // INP-001
		{"description_interior_case", "POST", "/api/orders", "{\"description\":\"Caf\u00e9  CAFe\u0301\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},                 // INP-001
		{"description_leading_unicode", "POST", "/api/orders", "{\"description\":\"\u2003item\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                          // INP-001
		{"description_trailing_unicode", "POST", "/api/orders", "{\"description\":\"item\u00a0\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                         // INP-001
		{"description_control", "POST", "/api/orders", "{\"description\":\"item\u0085\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                  // INP-001
		{"description_newline", "POST", "/api/orders", "{\"description\":\"item\\n\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                     // INP-001
		{"description_null", "POST", "/api/orders", "{\"description\":null,\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                               // INP-001
		{"description_number", "POST", "/api/orders", "{\"description\":123,\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                              // INP-001
		{"description_missing", "POST", "/api/orders", "{\"amount\": 2500, \"request_key\": \"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                                              // INP-001
		{"invalid_utf8", "POST", "/api/orders", "{\"description\":\"Single caf\xff product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                             // INP-001
		{"amount_minimum", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":50,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},                           // INP-002
		{"amount_maximum", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":100000,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},                       // INP-002
		{"amount_below", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":49,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                             // INP-002
		{"amount_above", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":100001,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                         // INP-002
		{"amount_zero", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":0,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                               // INP-002
		{"amount_negative", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":-50,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                         // INP-002
		{"amount_string", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":\"2500\",\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                      // INP-002
		{"amount_fraction", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500.0,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                      // INP-002
		{"amount_exponent", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":25e2,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                        // INP-002
		{"amount_null", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":null,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                            // INP-002
		{"amount_boolean", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":true,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                         // INP-002
		{"amount_overflow", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":9223372036854775808,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},         // INP-002
		{"amount_missing", "POST", "/api/orders", "{\"description\": \"Single caf\\u00e9 product\", \"request_key\": \"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                     // INP-002
		{"currency_override", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\",\"currency\":\"usd\"}", "application/json", "", 400}, // INP-002
		{"quantity_override", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\",\"quantity\":2}", "application/json", "", 400},       // INP-002
		{"key_uppercase", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-ABCD-4111-8111-111111111111\"}", "application/json", "", 400},                          // INP-003
		{"key_no_hyphens", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111111141118111111111111111\"}", "application/json", "", 400},                             // INP-003
		{"key_version", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-1111-8111-111111111111\"}", "application/json", "", 400},                            // INP-003
		{"key_variant", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-1111-111111111111\"}", "application/json", "", 400},                            // INP-003
		{"key_length", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"short\"}", "application/json", "", 400},                                                            // INP-003
		{"key_null", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":null}", "application/json", "", 400},                                                                   // INP-003
		{"key_number", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":123}", "application/json", "", 400},                                                                  // INP-003
		{"key_missing", "POST", "/api/orders", "{\"description\": \"Single caf\\u00e9 product\", \"amount\": 2500}", "application/json", "", 400},                                                                                 // INP-003
		{"trailing_whitespace", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"} \n\t", "application/json", "", 201},               // INP-004
		{"duplicate", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\",\"amount\":2500}", "application/json", "", 400},              // INP-004
		{"unknown", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\",\"extra\":1}", "application/json", "", 400},                    // INP-004
		{"array", "POST", "/api/orders", "[]", "application/json", "", 400},    // INP-004
		{"scalar", "POST", "/api/orders", "1", "application/json", "", 400},    // INP-004
		{"null", "POST", "/api/orders", "null", "application/json", "", 400},   // INP-004
		{"empty", "POST", "/api/orders", "", "application/json", "", 400},      // INP-004
		{"malformed", "POST", "/api/orders", "{", "application/json", "", 400}, // INP-004
		{"second_value", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"} {}", "application/json", "", 400},                 // INP-004
		{"trailing_junk", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"} x", "application/json", "", 400},                 // INP-004
		{"media_json", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 201},                      // INP-005
		{"media_charset", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json; charset=utf-8", "", 201},    // INP-005
		{"media_missing", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "", "", 415},                                   // INP-005
		{"media_text", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "text/plain", "", 415},                            // INP-005
		{"media_latin", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json; charset=iso-8859-1", "", 415}, // INP-005
		{"encoding_gzip", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "gzip", 415},               // INP-005
		{"exact_4096", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                       ", "application/json", "", 201}, // INP-005
		{"over_4097", "POST", "/api/orders", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        ", "application/json", "", 413}, // INP-005
		{"GET_empty", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", "", "application/json", "", 200},                                                                                                                                                // INP-006
		{"GET_body", "GET", "/api/orders/11111111-1111-4111-8111-111111111111", "x", "application/json", "", 400},                                                                                                                                                // INP-006
		{"GET_query", "GET", "/api/orders/11111111-1111-4111-8111-111111111111?extra=1", "", "application/json", "", 400},                                                                                                                                        // INP-006
		{"GET_history_default", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history", "", "application/json", "", 200},                                                                                                                              // INP-006
		{"GET_history_boundaries", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=9223372036854775807&limit=100", "", "application/json", "", 200},                                                                                       // INP-006
		{"GET_history_min", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=0&limit=1", "", "application/json", "", 200},                                                                                                                  // INP-006
		{"GET_negative_after", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=-1", "", "application/json", "", 400},                                                                                                                      // INP-006
		{"GET_overflow_after", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=9223372036854775808", "", "application/json", "", 400},                                                                                                     // INP-006
		{"GET_duplicate_after", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=0&after=1", "", "application/json", "", 400},                                                                                                              // INP-006
		{"GET_unknown_query", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?unknown=1", "", "application/json", "", 400},                                                                                                                      // INP-006
		{"GET_zero_limit", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=0", "", "application/json", "", 400},                                                                                                                           // INP-006
		{"GET_over_limit", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=101", "", "application/json", "", 400},                                                                                                                         // INP-006
		{"GET_bad_limit", "GET", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=1.5", "", "application/json", "", 400},                                                                                                                          // INP-006
		{"HEAD_empty", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111", "", "application/json", "", 200},                                                                                                                                              // INP-006
		{"HEAD_body", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111", "x", "application/json", "", 400},                                                                                                                                              // INP-006
		{"HEAD_query", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111?extra=1", "", "application/json", "", 400},                                                                                                                                      // INP-006
		{"HEAD_history_default", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history", "", "application/json", "", 200},                                                                                                                            // INP-006
		{"HEAD_history_boundaries", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=9223372036854775807&limit=100", "", "application/json", "", 200},                                                                                     // INP-006
		{"HEAD_history_min", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=0&limit=1", "", "application/json", "", 200},                                                                                                                // INP-006
		{"HEAD_negative_after", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=-1", "", "application/json", "", 400},                                                                                                                    // INP-006
		{"HEAD_overflow_after", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=9223372036854775808", "", "application/json", "", 400},                                                                                                   // INP-006
		{"HEAD_duplicate_after", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?after=0&after=1", "", "application/json", "", 400},                                                                                                            // INP-006
		{"HEAD_unknown_query", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?unknown=1", "", "application/json", "", 400},                                                                                                                    // INP-006
		{"HEAD_zero_limit", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=0", "", "application/json", "", 400},                                                                                                                         // INP-006
		{"HEAD_over_limit", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=101", "", "application/json", "", 400},                                                                                                                       // INP-006
		{"HEAD_bad_limit", "HEAD", "/api/orders/11111111-1111-4111-8111-111111111111/history?limit=1.5", "", "application/json", "", 400},                                                                                                                        // INP-006
		{"creation_query", "POST", "/api/orders?x=1", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400},                                                    // INP-006
		{"checkout_initial_fields", "POST", "/api/orders/11111111-1111-4111-8111-111111111111/checkout", "{\"description\":\"Single caf\u00e9 product\",\"amount\":2500,\"request_key\":\"11111111-1111-4111-8111-111111111111\"}", "application/json", "", 400}, // INP-004
		{"malformed_order", "GET", "/api/orders/bad", "", "application/json", "", 400},                                                                                                                                                                           // INP-003
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, logs := checkoutHandler(t, f, 10*time.Second)
			r := testutil.Request(tc.method, tc.path, tc.body, true)
			r.Header.Set("Content-Type", tc.media)
			if tc.encoding != "" {
				r.Header.Set("Content-Encoding", tc.encoding)
			}
			w := testutil.Response(t, h, r)
			require.Equal(t, tc.status, w.Code)
			if tc.status >= 400 {
				testutil.RequireRejectedLog(t, logs, w.Header().Get("X-Request-ID"), w.Code, "", "", tc.body)
				require.Zero(t, f.Effects())
				if tc.method != "HEAD" {
					code := "invalid_request"
					if tc.status == 413 {
						code = "body_too_large"
					}
					if tc.status == 415 {
						code = "unsupported_media_type"
					}
					checkoutLocalError(t, w, code)
				}
			} else {
				require.EqualValues(t, 1, f.Effects())
				if tc.method == "POST" {
					var expected payment.CreateInput
					var raw struct {
						Description string `json:"description"`
						Amount      int64  `json:"amount"`
						Key         string `json:"request_key"`
					}
					require.NoError(t, json.Unmarshal([]byte(tc.body), &raw))
					expected.Description = raw.Description
					expected.Amount = raw.Amount
					expected.RequestKey = raw.Key
					require.Equal(t, expected, f.Input)
				}
			}
			if tc.method == "HEAD" {
				require.Empty(t, w.Body.String())
			}
		})
	}
}
func TestCheckoutReadFailures(t *testing.T) { // INP-004 INP-006
	for _, tc := range []struct {
		name, method, data string
		err                error
	}{{"post_zero_error", "POST", "", errors.New("read-sentinel")}, {"post_partial_error", "POST", `{"description":"x"`, errors.New("read-sentinel")}, {"read_zero_error", "GET", "", errors.New("read-sentinel")}, {"read_normal_eof", "GET", "", io.EOF}} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, time.Second)
			path := "/api/orders"
			if tc.method == "GET" {
				path += "/" + f.View.Order.ID
			}
			r := testutil.Request(tc.method, path, "", true)
			r.Body = &checkoutReadFault{Data: []byte(tc.data), Error: tc.err}
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			if tc.err == io.EOF {
				require.Equal(t, 200, w.Code)
			} else {
				require.Equal(t, 400, w.Code)
				require.Zero(t, f.Effects())
				require.NotContains(t, w.Body.String(), "read-sentinel")
			}
		})
	}
}

func TestCheckoutContinuationValidation(t *testing.T) { // INP-003 INP-004 INP-005 HTTP-004
	for _, tc := range []struct {
		name, body, media, encoding string
		status                      int
	}{
		{"valid", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json", "", 201},
		{"charset", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json; charset=utf-8", "", 201},
		{"trailing_whitespace", `{"request_key":"11111111-1111-4111-8111-111111111111"}` + " \n\t", "application/json", "", 201},
		{"missing", `{}`, "application/json", "", 400},
		{"short_key", `{"request_key":"short"}`, "application/json", "", 400},
		{"empty_body", ``, "application/json", "", 400},
		{"trailing_junk", `{"request_key":"11111111-1111-4111-8111-111111111111"} x`, "application/json", "", 400},
		{"invalid_utf8", "{\"request_key\":\"\xff\"}", "application/json", "", 400},
		{"null_key", `{"request_key":null}`, "application/json", "", 400},
		{"empty_key", `{"request_key":""}`, "application/json", "", 400},
		{"uppercase", `{"request_key":"ABCDEFAB-1111-4111-8111-111111111111"}`, "application/json", "", 400},
		{"version", `{"request_key":"11111111-1111-5111-8111-111111111111"}`, "application/json", "", 400},
		{"variant", `{"request_key":"11111111-1111-4111-7111-111111111111"}`, "application/json", "", 400},
		{"unhyphenated", `{"request_key":"11111111111141118111111111111111"}`, "application/json", "", 400},
		{"number", `{"request_key":42}`, "application/json", "", 400},
		{"duplicate", `{"request_key":"11111111-1111-4111-8111-111111111111","request_key":"22222222-2222-4222-8222-222222222222"}`, "application/json", "", 400},
		{"unknown", `{"request_key":"11111111-1111-4111-8111-111111111111","x":1}`, "application/json", "", 400},
		{"array", `[]`, "application/json", "", 400},
		{"null_object", `null`, "application/json", "", 400},
		{"scalar", `1`, "application/json", "", 400},
		{"second_object", `{"request_key":"11111111-1111-4111-8111-111111111111"} {}`, "application/json", "", 400},
		{"partial", `{"request_key":"11111111-1111-4111-8111-111111111111"`, "application/json", "", 400},
		{"media_missing", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "", "", 415},
		{"media_wrong", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "text/plain", "", 415},
		{"charset_wrong", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json; charset=iso-8859-1", "", 415},
		{"encoding", `{"request_key":"11111111-1111-4111-8111-111111111111"}`, "application/json", "gzip", 415},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, 2*time.Second)
			r := testutil.Request("POST", "/api/orders/"+f.Outcome.Order.ID+"/checkout", tc.body, true)
			r.Header.Set("Content-Type", tc.media)
			r.Header.Set("Content-Encoding", tc.encoding)
			w := testutil.Response(t, h, r)
			require.Equal(t, tc.status, w.Code)
			if tc.status >= 400 {
				require.Zero(t, f.Effects())
				code := "invalid_request"
				if tc.status == 415 {
					code = "unsupported_media_type"
				}
				checkoutLocalError(t, w, code)
			} else {
				require.EqualValues(t, 1, f.Continued.Load())
				require.Equal(t, f.Outcome.Order.ID, f.OrderID)
				require.Equal(t, "11111111-1111-4111-8111-111111111111", f.ContinueKey)
			}
		})
	}
}
func TestCheckoutDuplicateFields(t *testing.T) { // INP-004 HTTP-004
	for _, tc := range []struct{ name, body string }{
		{"description", `{"description":"a","description":"b","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`},
		{"request_key", `{"description":"a","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111","request_key":"22222222-2222-4222-8222-222222222222"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, 2*time.Second)
			w := testutil.Response(t, h, testutil.Request("POST", "/api/orders", tc.body, true))
			require.Equal(t, 400, w.Code)
			checkoutLocalError(t, w, "invalid_request")
			require.Zero(t, f.Effects())
		})
	}
}
func TestCheckoutCompleteJSONReadError(t *testing.T) { // INP-004 HTTP-004
	for _, tc := range []struct{ name, path, body string }{
		{"initial", "/api/orders", `{"description":"a","amount":2500,"request_key":"11111111-1111-4111-8111-111111111111"}`},
		{"continuation", "/api/orders/11111111-1111-4111-8111-111111111111/checkout", `{"request_key":"11111111-1111-4111-8111-111111111111"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, 2*time.Second)
			r := testutil.Request("POST", tc.path, "", true)
			r.Body = &checkoutReadFault{Data: []byte(tc.body), Error: errors.New("read-sentinel")}
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			require.Equal(t, 400, w.Code)
			checkoutLocalError(t, w, "invalid_request")
			require.Zero(t, f.Effects())
		})
	}
}
func TestCheckoutUnknownLengthReadBodies(t *testing.T) { // INP-006 HTTP-004
	for _, tc := range []struct {
		name, method, data string
		err                error
		status             int
	}{
		{"get_empty", "GET", "", io.EOF, 200}, {"get_nonempty", "GET", "x", io.EOF, 400},
		{"head_empty", "HEAD", "", io.EOF, 200}, {"head_nonempty", "HEAD", "x", io.EOF, 400},
		{"head_read_error", "HEAD", "", errors.New("read-sentinel"), 400},
		{"get_large_body", "GET", strings.Repeat("x", 1<<20), io.EOF, 400},
		{"head_large_body", "HEAD", strings.Repeat("x", 1<<20), io.EOF, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, 2*time.Second)
			r := testutil.Request(tc.method, "/api/orders/"+f.View.Order.ID, "", true)
			reader := &checkoutReadFault{Data: []byte(tc.data), Error: tc.err}
			r.Body = reader
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			require.Equal(t, tc.status, w.Code)
			require.Greater(t, reader.Reads.Load(), int32(0))
			require.LessOrEqual(t, reader.Bytes.Load(), int64(4097), "read endpoint detection must stay bounded regardless of submitted size")
			if tc.status >= 400 {
				require.Zero(t, f.Effects())
				checkoutHeaders(t, w)
				if tc.method == "GET" {
					checkoutLocalError(t, w, "invalid_request")
				}
			} else {
				require.EqualValues(t, 1, f.Read.Load())
			}
			if tc.method == "HEAD" {
				require.Empty(t, w.Body.String())
			}
		})
	}
}

func TestCheckoutContinuationReadFailures(t *testing.T) { // INP-004 INP-005 HTTP-004
	for _, tc := range []struct {
		name, data string
		err        error
	}{
		{"zero_read_error", "", errors.New("read-sentinel")}, {"partial_read_error", `{"request_key":"11111111-1111`, errors.New("read-sentinel")}, {"zero_normal_eof", "", io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := preparedCheckout()
			h, _ := checkoutHandler(t, f, 2*time.Second)
			r := testutil.Request("POST", "/api/orders/"+f.Outcome.Order.ID+"/checkout", "", true)
			r.Body = &checkoutReadFault{Data: []byte(tc.data), Error: tc.err}
			r.ContentLength = -1
			w := testutil.Response(t, h, r)
			require.Equal(t, 400, w.Code)
			require.Zero(t, f.Effects())
			checkoutLocalError(t, w, "invalid_request")
		})
	}
}
