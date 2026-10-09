package main

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestRunRejectsMissingConfiguration(t *testing.T) {
	var out bytes.Buffer
	err := run(context.Background(), nil, func(string) string { return "" }, &out)
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
