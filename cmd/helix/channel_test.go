// Command helix — channel_test.go
//
// Tests for `helix channel` (create/join/send/list/archive/history) —
// SPEC-024 §8.
package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/totalwindupflightsystems/helix/pkg/channel"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// setChannelsFileEnv points HELIX_CHANNELS_FILE at a path under a fresh
// temp dir and returns the path. The env is restored automatically by
// t.Setenv.
func setChannelsFileEnv(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "channels.yaml")
	t.Setenv(envChannelFile, p)
	return p
}

// writeChannelsFile writes the given channels.yaml content to path and
// returns the path.
func writeChannelsFile(t *testing.T, path, content string) string {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// createChannelViaCLI drives `helix channel create` and asserts success.
func createChannelViaCLI(t *testing.T, name, typ, members string) {
	t.Helper()
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", name, "--type", typ, "--members", members},
		&stdout, &stderr)
	require.Equal(t, channelExitOK, rc, "create %s: stderr: %s", name, stderr.String())
}

// sendViaCLI drives `helix channel send` and asserts success.
func sendViaCLI(t *testing.T, chName, msg string) {
	t.Helper()
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", chName, "--message", msg}, &stdout, &stderr)
	require.Equal(t, channelExitOK, rc, "send to %s: stderr: %s", chName, stderr.String())
}

// storedIdentity rebuilds the identity.AgentIdentity persisted in the
// channels file (used to verify message signatures).
func storedIdentity(t *testing.T, path string) *identity.AgentIdentity {
	t.Helper()
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	require.NotNil(t, file.Identity, "channels file must contain an identity")
	pub, err := base64.StdEncoding.DecodeString(file.Identity.PubKey)
	require.NoError(t, err, "stored identity pubkey must be valid base64")
	return &identity.AgentIdentity{ID: file.Identity.AgentID, PubKey: pub}
}

// storedMessages returns the channel messages persisted in the channels
// file for the given channel name.
func storedMessages(t *testing.T, path, chName string) []*channel.ChannelMessage {
	t.Helper()
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	ch := findChannelByName(file, chName)
	require.NotNil(t, ch, "channel %q must exist", chName)
	var out []*channel.ChannelMessage
	for _, my := range file.Messages {
		if my.ChannelID == ch.ID {
			m, err := my.toMessage()
			require.NoError(t, err, "stored message must decode")
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// parseChannelFlags
// ---------------------------------------------------------------------------

func TestParseChannelFlags(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		want       channelFlags
		wantHelp   bool
		wantExitOK bool
	}{
		{
			name: "create all flags",
			args: []string{"create", "--name", "auth", "--type", "task",
				"--members", "a, b"},
			want:       channelFlags{subcommand: "create", name: "auth", typ: "task", members: "a, b"},
			wantExitOK: true,
		},
		{
			name: "send all flags",
			args: []string{"send", "--channel", "auth", "--message", "hi",
				"--attachment", "/tmp/x"},
			want:       channelFlags{subcommand: "send", channel: "auth", message: "hi", attachment: "/tmp/x"},
			wantExitOK: true,
		},
		{
			name:       "list with status",
			args:       []string{"list", "--status", "archived"},
			want:       channelFlags{subcommand: "list", status: "archived"},
			wantExitOK: true,
		},
		{
			name:       "history with limit",
			args:       []string{"history", "--name", "auth", "--limit", "5"},
			want:       channelFlags{subcommand: "history", name: "auth", limit: 5},
			wantExitOK: true,
		},
		{
			name:       "help",
			args:       []string{"--help"},
			wantHelp:   true,
			wantExitOK: true,
		},
		{
			name:       "missing flag value",
			args:       []string{"create", "--name"},
			wantExitOK: false,
		},
		{
			name:       "invalid limit",
			args:       []string{"history", "--name", "c", "--limit", "abc"},
			wantExitOK: false,
		},
		{
			name:       "unknown flag",
			args:       []string{"list", "--bogus"},
			wantExitOK: false,
		},
		{
			name:       "extra positional",
			args:       []string{"create", "extra"},
			wantExitOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, help, exitCode := parseChannelFlags(tt.args)
			assert.Equal(t, tt.wantHelp, help)
			if tt.wantExitOK {
				assert.Equal(t, channelExitOK, exitCode)
				assert.Equal(t, tt.want, got)
			} else {
				assert.Equal(t, channelExitError, exitCode)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runChannel — help / dispatch
// ---------------------------------------------------------------------------

func TestRunChannel_Help(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"--help"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "helix channel")
	assert.Contains(t, stdout.String(), "create")
	assert.Contains(t, stdout.String(), "join")
	assert.Contains(t, stdout.String(), "send")
	assert.Contains(t, stdout.String(), "list")
	assert.Contains(t, stdout.String(), "archive")
	assert.Contains(t, stdout.String(), "history")
}

func TestRunChannel_NoArgsShowsHelp(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "helix channel")
}

func TestRunChannel_UnknownSubcommand(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"bogus"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "unknown subcommand")
}

// ---------------------------------------------------------------------------
// runChannelCreate
// ---------------------------------------------------------------------------

func TestRunChannelCreate_MissingName(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--type", "task"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "--name is required")
}

func TestRunChannelCreate_MissingType(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "--type is required")
}

func TestRunChannelCreate_InvalidType(t *testing.T) {
	path := setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "auth", "--type", "bogus"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), `invalid type "bogus"`)
	// The file must NOT have been created.
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "channels file must not be written on validation failure")
}

func TestRunChannelCreate_Success(t *testing.T) {
	path := setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "auth", "--type", "task",
		"--members", "agent-a, agent-b"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), `channel "auth" created`)

	file, err := loadChannelFile(path)
	require.NoError(t, err)
	require.Len(t, file.Channels, 1)
	assert.Equal(t, "auth", file.Channels[0].Name)
	assert.Equal(t, channel.ChannelTask, file.Channels[0].Type)
	assert.Equal(t, channel.ChannelStatusActive, file.Channels[0].Status)
	assert.Equal(t, []string{"agent-a", "agent-b"}, file.Channels[0].Members)
	assert.NotEmpty(t, file.Channels[0].ID)
}

func TestRunChannelCreate_EmptyMembersOK(t *testing.T) {
	path := setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "lobby", "--type", "review"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Empty(t, file.Channels[0].Members)
}

func TestRunChannelCreate_Duplicate(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "auth", "--type", "task"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "already exists")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Len(t, file.Channels, 1, "duplicate create must not add a channel")
}

func TestRunChannelCreate_DryRunNoWrite(t *testing.T) {
	path := setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"create", "--name", "auth", "--type", "task",
		"--members", "a", "--dry-run"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "[DRY-RUN]")
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "dry-run must not write the channels file")
}

// ---------------------------------------------------------------------------
// runChannelJoin
// ---------------------------------------------------------------------------

func TestRunChannelJoin_NotFound(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"join", "--name", "nope"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Nil(t, file.Identity, "failed join must not create an identity")
}

func TestRunChannelJoin_AddsMemberAndIdentity(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "agent-a")

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"join", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "joined channel")

	file, err := loadChannelFile(path)
	require.NoError(t, err)
	require.NotNil(t, file.Identity, "join must create the CLI identity")
	assert.Equal(t, cliIdentityName, file.Identity.Name)
	assert.NotEmpty(t, file.Identity.AgentID)
	pub, err := base64.StdEncoding.DecodeString(file.Identity.PubKey)
	require.NoError(t, err)
	assert.Len(t, pub, 32)
	priv, err := base64.StdEncoding.DecodeString(file.Identity.PrivateKey)
	require.NoError(t, err)
	assert.Len(t, priv, 64)
	assert.Equal(t, []string{"agent-a", cliIdentityName}, file.Channels[0].Members)

	// The stored identity must be usable: its fingerprint round-trips.
	id := storedIdentity(t, path)
	assert.Equal(t, file.Identity.AgentID, id.ID)
}

func TestRunChannelJoin_Idempotent(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"join", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)

	// Second join: already a member — exit 0, no-op.
	stdout.Reset()
	rc = runChannel([]string{"join", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "already a member")

	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"a", cliIdentityName}, file.Channels[0].Members,
		"idempotent join must not duplicate the member")
}

func TestRunChannelJoin_Archived(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	require.Equal(t, channelExitOK, rc, "archive should succeed on empty channel: %s", stderr.String())

	stdout.Reset()
	rc = runChannel([]string{"join", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "is archived")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, file.Channels[0].Members, "join to archived channel must not add members")
}

// ---------------------------------------------------------------------------
// runChannelSend
// ---------------------------------------------------------------------------

func TestRunChannelSend_MissingChannel(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--message", "hi"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "--channel is required")
}

func TestRunChannelSend_MissingMessage(t *testing.T) {
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "--message is required")
}

func TestRunChannelSend_NotFound(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "nope", "--message", "hi"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Len(t, file.Messages, 0)
}

func TestRunChannelSend_PersistsAndSigns(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "auth", "--message", "hello world"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), `message sent to "auth"`)

	msgs := storedMessages(t, path, "auth")
	require.Len(t, msgs, 1)
	m := msgs[0]
	assert.Equal(t, cliIdentityName, m.Author)
	assert.Equal(t, channel.AuthorAgent, m.AuthorType)
	assert.Equal(t, channel.MsgText, m.Type)
	assert.Equal(t, "hello world", m.Content)
	assert.NotNil(t, m.HIDProof, "sent message must carry an HID proof")
	assert.NotEmpty(t, m.HIDProof.SigBytes)
	assert.Equal(t, storedIdentity(t, path).Fingerprint(), m.HIDProof.Fingerprint)
	require.NoError(t, channel.VerifyMessage(m, storedIdentity(t, path)),
		"message must verify against the stored CLI identity")
}

func TestRunChannelSend_Attachment(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")

	attPath := filepath.Join(t.TempDir(), "evidence.bin")
	payload := []byte{0xde, 0xad, 0xbe, 0xef, 0x01, 0x02}
	require.NoError(t, os.WriteFile(attPath, payload, 0o644))

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "auth", "--message", "see attachment",
		"--attachment", attPath}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)

	msgs := storedMessages(t, path, "auth")
	require.Len(t, msgs, 1)
	require.Len(t, msgs[0].Attachments, 1)
	assert.Equal(t, "evidence.bin", msgs[0].Attachments[0].Name)
	assert.Equal(t, payload, msgs[0].Attachments[0].Data)
	require.NoError(t, channel.VerifyMessage(msgs[0], storedIdentity(t, path)),
		"attachments are part of the signed payload")
}

func TestRunChannelSend_MissingAttachmentFile(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "auth", "--message", "hi",
		"--attachment", filepath.Join(t.TempDir(), "nope.bin")}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "read attachment")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Len(t, file.Messages, 0, "failed send must not persist a message")
}

func TestRunChannelSend_ToArchivedChannel(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "before archive")

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	require.Equal(t, channelExitOK, rc)

	stdout.Reset()
	rc = runChannel([]string{"send", "--channel", "auth", "--message", "too late"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "is archived")
	msgs := storedMessages(t, path, "auth")
	assert.Len(t, msgs, 1, "send to archived channel must not persist")
}

func TestRunChannelSend_DryRunNoWrite(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"send", "--channel", "auth", "--message", "hi", "--dry-run"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "[DRY-RUN]")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Len(t, file.Messages, 0, "dry-run must not persist a message")
	assert.Nil(t, file.Identity, "dry-run must not create the identity")
}

// ---------------------------------------------------------------------------
// runChannelList
// ---------------------------------------------------------------------------

func TestRunChannelList_Empty(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "no channels")
}

func TestRunChannelList_InvalidStatus(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"list", "--status", "bogus"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "invalid status")
}

func TestRunChannelList_SortedAndStatusFilter(t *testing.T) {
	setChannelsFileEnv(t)
	createChannelViaCLI(t, "beta", "review", "b1,b2")
	createChannelViaCLI(t, "alpha", "task", "a1")

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "beta"}, &stdout, &stderr)
	require.Equal(t, channelExitOK, rc)

	// Unfiltered: both channels, sorted by name, with header.
	stdout.Reset()
	rc = runChannel([]string{"list"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out := stdout.String()
	assert.Contains(t, out, "NAME")
	assert.Contains(t, out, "TYPE")
	assert.Contains(t, out, "STATUS")
	assert.Contains(t, out, "MEMBERS")
	aIdx := strings.Index(out, "alpha")
	bIdx := strings.Index(out, "beta")
	assert.True(t, aIdx >= 0 && bIdx > aIdx, "rows must be sorted by name")

	// --status active: only alpha.
	stdout.Reset()
	rc = runChannel([]string{"list", "--status", "active"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out = stdout.String()
	assert.Contains(t, out, "alpha")
	assert.NotContains(t, out, "beta")

	// --status archived: only beta.
	stdout.Reset()
	rc = runChannel([]string{"list", "--status", "archived"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out = stdout.String()
	assert.NotContains(t, out, "alpha")
	assert.Contains(t, out, "beta")

	// Hand-written file with an archived channel parses and filters too.
	other := setChannelsFileEnv(t)
	writeChannelsFile(t, other, `channels:
  - id: ch-1
    name: zebra
    type: incident
    status: archived
    members: [m1]
    created_at: 2026-08-01T00:00:00Z
    updated_at: 2026-08-01T00:00:00Z
`)
	stdout.Reset()
	rc = runChannel([]string{"list", "--status", "archived"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "zebra")
}

// ---------------------------------------------------------------------------
// runChannelHistory
// ---------------------------------------------------------------------------

func TestRunChannelHistory_NotFound(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"history", "--name", "nope"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunChannelHistory_Empty(t *testing.T) {
	setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"history", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "no messages")
}

func TestRunChannelHistory_OrderingAndLimit(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "m1")
	sendViaCLI(t, "auth", "m2")
	sendViaCLI(t, "auth", "m3")

	// Full history: ascending order.
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"history", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out := stdout.String()
	i1, i2, i3 := strings.Index(out, "  m1\n"), strings.Index(out, "  m2\n"), strings.Index(out, "  m3\n")
	assert.True(t, i1 >= 0 && i2 > i1 && i3 > i2, "history must be ordered ascending, got: %s", out)
	assert.Contains(t, out, "cli", "history lines must include the author")

	// --limit 2: most-recent two, in order, oldest dropped.
	stdout.Reset()
	rc = runChannel([]string{"history", "--name", "auth", "--limit", "2"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out = stdout.String()
	assert.NotContains(t, out, "  m1\n")
	assert.Contains(t, out, "  m2\n")
	assert.Contains(t, out, "  m3\n")
	i2, i3 = strings.Index(out, "  m2\n"), strings.Index(out, "  m3\n")
	assert.True(t, i2 >= 0 && i3 > i2, "limited history must keep ascending order")

	// Default limit (20): 25 messages → only the last 20.
	for n := 4; n <= 25; n++ {
		sendViaCLI(t, "auth", "msg-"+pad2(n))
	}
	stdout.Reset()
	rc = runChannel([]string{"history", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	out = stdout.String()
	assert.NotContains(t, out, "msg-04")
	assert.Contains(t, out, "msg-06", "first kept message should be msg-05 or later")
	assert.Contains(t, out, "msg-25", "newest message must be present")
	lines := strings.Count(out, "\n") // each printed line ends with "\n"
	assert.Equal(t, 20, lines, "default limit must truncate to 20 lines")

	// The three original messages are still in the file (limit is display-only).
	msgs := storedMessages(t, path, "auth")
	assert.Len(t, msgs, 25)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// ---------------------------------------------------------------------------
// runChannelArchive
// ---------------------------------------------------------------------------

func TestRunChannelArchive_NotFound(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "nope"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "not found")
}

func TestRunChannelArchive_EmptyChannel(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "0 written")
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, channel.ChannelStatusArchived, file.Channels[0].Status)
}

func TestRunChannelArchive_VerifiesAndMarksArchived(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "hello")

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), `archived "auth"`)
	assert.Contains(t, stdout.String(), "1 written")
	assert.Contains(t, stdout.String(), "idempotent")

	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, channel.ChannelStatusArchived, file.Channels[0].Status)
	assert.Len(t, file.Messages, 1, "archival must keep the message log in the file")
}

func TestRunChannelArchive_AlreadyArchivedIsIdempotent(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "hello")

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	require.Equal(t, channelExitOK, rc)

	// Second archive: re-verify + report, exit 0, no state change.
	stdout.Reset()
	rc = runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitOK, rc)
	assert.Contains(t, stdout.String(), "already archived")
	assert.Contains(t, stdout.String(), "re-verified 1 message")

	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, channel.ChannelStatusArchived, file.Channels[0].Status)
	assert.Len(t, file.Messages, 1)
}

func TestRunChannelArchive_TamperedMessageFailsClosed(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "original-content-abc123")

	// Tamper with the message content — the HID signature no longer matches.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	tampered := strings.ReplaceAll(string(data), "original-content-abc123", "TAMPERED-CONTENT-xyz")
	require.NotEqual(t, string(data), tampered, "tamper replacement must change the file")
	require.NoError(t, os.WriteFile(path, []byte(tampered), 0o644))

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "failed verification")

	// Fail closed: channel must NOT be marked archived, no state corruption.
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, channel.ChannelStatusActive, file.Channels[0].Status,
		"tampered archival must not mark the channel archived")
	assert.Len(t, file.Messages, 1, "tampered archival must not drop messages")
}

func TestRunChannelArchive_UnsignedMessageFailsClosed(t *testing.T) {
	path := setChannelsFileEnv(t)
	createChannelViaCLI(t, "auth", "task", "a")
	sendViaCLI(t, "auth", "signed message")

	// Hand-append an unsigned message to the file.
	file, err := loadChannelFile(path)
	require.NoError(t, err)
	ch := findChannelByName(file, "auth")
	unsigned := channel.NewChannelMessage(ch.ID, "someone", channel.AuthorAgent, channel.MsgText, "forged")
	file.Messages = append(file.Messages, toMessageYAML(unsigned))
	require.NoError(t, saveChannelFile(path, file))

	var stdout, stderr strings.Builder
	rc := runChannel([]string{"archive", "--name", "auth"}, &stdout, &stderr)
	assert.Equal(t, channelExitError, rc)
	assert.Contains(t, stderr.String(), "no HID proof")

	reloaded, err := loadChannelFile(path)
	require.NoError(t, err)
	assert.Equal(t, channel.ChannelStatusActive, reloaded.Channels[0].Status)
	assert.Len(t, reloaded.Messages, 2, "failed archival must not drop messages")
}

// ---------------------------------------------------------------------------
// runChannelWithDryRun — errExit contract
// ---------------------------------------------------------------------------

func TestRunChannelWithDryRun_ErrExitOnFailure(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	err := runChannelWithDryRun([]string{"archive", "--name", "nope"}, &stdout, &stderr, false)
	require.Error(t, err)
	var ee errExit
	assert.True(t, errors.As(err, &ee), "error must wrap errExit")
	assert.Equal(t, channelExitError, ee.code)
}

func TestRunChannelWithDryRun_NilOnSuccess(t *testing.T) {
	setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	err := runChannelWithDryRun([]string{"list"}, &stdout, &stderr, false)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "no channels")
}

func TestRunChannelWithDryRun_ThreadsGlobalDryRun(t *testing.T) {
	path := setChannelsFileEnv(t)
	var stdout, stderr strings.Builder
	err := runChannelWithDryRun([]string{"create", "--name", "auth", "--type", "task"},
		&stdout, &stderr, true)
	assert.NoError(t, err)
	assert.Contains(t, stdout.String(), "[DRY-RUN]")
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "global dry-run must not write the channels file")
}
