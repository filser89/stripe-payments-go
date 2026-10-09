package main

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRunRejectsMissingConfiguration(t *testing.T) { // CFG-001 SEC-001
	var out bytes.Buffer
	err := run(context.Background(), nil, func(k string) string {
		switch k {
		case "BASIC_AUTH_USERNAME":
			return "command-fixture-user"
		case "BASIC_AUTH_PASSWORD":
			return "command-fixture-password"
		default:
			return ""
		}
	}, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "DATABASE_URL")
}
func TestRunRejectsInvalidDatabaseWithoutExposingCredentials(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), nil, func(k string) string {
		if k == "DATABASE_URL" {
			return "postgres://user:SECRET@%gg"
		}
		return ""
	}, &out)
	require.Error(t, err)
	require.NotContains(t, err.Error()+out.String(), "SECRET")
}
func TestRunRejectsUnknownCommandBeforeConnecting(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), []string{"unknown"}, func(k string) string {
		if k == "DATABASE_URL" {
			return "postgres://user:SECRET@localhost/db"
		}
		return ""
	}, &out)
	require.Error(t, err)
	require.Contains(t, err.Error(), "command")
	require.NotContains(t, err.Error()+out.String(), "SECRET")
}
