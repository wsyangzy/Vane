package app

import (
	"context"
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wsyangzy/Vane/internal/providers/claude/proxy/handlers"
	claudeservice "github.com/wsyangzy/Vane/internal/providers/claude/service"
)

func TestClaudeEndpointTimeoutFollowsGlobalOnlyWhenNoOverrideIsSaved(t *testing.T) {
	a, members := poolTestApp(t, "claude")
	a.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	a.endpointService = claudeservice.NewEndpointService(a.endpointStore, a.endpointManager, a.config, a.claudeCredentialGroupStore)
	a.config.GlobalTimeout = 17 * time.Second
	forwarder := handlers.NewForwarder(a.config, nil)

	for _, seconds := range []int{0, 300, 45} {
		input := CreateEndpointInput{
			Channel: "first", CredentialGroupID: members[0].GroupID,
			Name: strconv.Itoa(seconds), URL: "https://example.invalid/" + strconv.Itoa(seconds),
			Priority: -1, TimeoutSeconds: seconds,
		}
		require.NoError(t, a.CreateEndpointRecord(input))
		stored, err := a.endpointStore.GetByChannelAndName(context.Background(), input.Channel, input.Name)
		require.NoError(t, err)
		require.NotNil(t, stored)
		require.Equal(t, seconds, stored.TimeoutSeconds)

		client, err := forwarder.NewClient(time.Duration(stored.TimeoutSeconds) * time.Second)
		require.NoError(t, err)
		expected := time.Duration(seconds) * time.Second
		if seconds == 0 {
			expected = a.config.GlobalTimeout
		}
		require.Equal(t, expected, client.Timeout)

		input.TimeoutSeconds = 0
		require.NoError(t, a.UpdateEndpointRecordByID(stored.ID, input))
		stored, err = a.endpointStore.GetByID(context.Background(), stored.ID)
		require.NoError(t, err)
		require.Zero(t, stored.TimeoutSeconds)
	}

	old, err := forwarder.NewClient(0)
	require.NoError(t, err)
	a.config.GlobalTimeout = 29 * time.Second
	current, err := forwarder.NewClient(0)
	require.NoError(t, err)
	require.Equal(t, 17*time.Second, old.Timeout)
	require.Equal(t, 29*time.Second, current.Timeout)
}
