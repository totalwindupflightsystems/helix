// Command helix — channel.go
//
// `helix channel` manages agent communication channels persisted in
// .helix/channels.yaml (SPEC-024 §8): create, join, send, list, archive,
// and history. Every outgoing message is signed with a persistent CLI
// agent identity (Ed25519, pkg/identity) so archival can verify provenance
// — the archive command fails closed on any unsigned or tampered message.
package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/totalwindupflightsystems/helix/pkg/channel"
	"github.com/totalwindupflightsystems/helix/pkg/identity"
	"github.com/totalwindupflightsystems/helix/pkg/memory"
)

const (
	channelExitOK    = 0
	channelExitError = 2

	// envChannelFile overrides the channels file path (default:
	// .helix/channels.yaml).
	envChannelFile = "HELIX_CHANNELS_FILE"

	// cliIdentityName is the display name of the persistent CLI agent
	// identity that signs channel messages and joins channels.
	cliIdentityName = "cli"

	// channelHistoryDefaultLimit is the number of most-recent messages
	// printed by `helix channel history` when --limit is not given.
	channelHistoryDefaultLimit = 20
)

// channelFlags holds parsed flags for helix channel subcommands.
type channelFlags struct {
	subcommand string
	name       string
	typ        string
	members    string
	channel    string
	message    string
	attachment string
	status     string
	limit      int
	dryRun     bool
}

// parseChannelFlags parses `helix channel` arguments. It returns the parsed
// flags, whether help was requested, and an exit code (channelExitOK on
// success, channelExitError on malformed arguments).
func parseChannelFlags(args []string) (channelFlags, bool, int) {
	var f channelFlags
	helpWanted := false

	i := 0
	for i < len(args) {
		arg := args[i]
		switch {
		case arg == "--help" || arg == "-h":
			helpWanted = true
		case arg == "--dry-run":
			f.dryRun = true
		case arg == "--name":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.name = args[i]
		case arg == "--type":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.typ = args[i]
		case arg == "--members":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.members = args[i]
		case arg == "--channel":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.channel = args[i]
		case arg == "--message":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.message = args[i]
		case arg == "--attachment":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.attachment = args[i]
		case arg == "--status":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			f.status = args[i]
		case arg == "--limit":
			i++
			if i >= len(args) {
				return f, false, channelExitError
			}
			n, err := strconv.Atoi(args[i])
			if err != nil {
				return f, false, channelExitError
			}
			f.limit = n
		default:
			if !strings.HasPrefix(arg, "-") && f.subcommand == "" {
				f.subcommand = arg
			} else {
				return f, false, channelExitError
			}
		}
		i++
	}
	return f, helpWanted, channelExitOK
}

const channelHelp = `helix channel — manage agent communication channels (.helix/channels.yaml)

Usage:
  helix channel create   --name STRING --type [task|review|deliberation|incident] [--members AGENT,AGENT]
  helix channel join     --name STRING
  helix channel send     --channel STRING --message STRING [--attachment PATH]
  helix channel list     [--status active|archived]
  helix channel archive  --name STRING
  helix channel history  --name STRING [--limit N]

Options:
  --name STRING          Channel name (create/join/archive/history)
  --type TYPE            Channel type: task | review | deliberation | incident
  --members LIST         Comma-separated initial members (create)
  --channel STRING       Channel name to send to
  --message STRING       Message body (type: text)
  --attachment PATH      File to attach to the message (binary-safe)
  --status STATUS        list: filter by status: active | archived
  --limit N              history: most-recent N messages (default: 20;
                         --limit 0 resets to the default)
  --dry-run              create/join/send: show what would be done without writing

Environment:
  HELIX_CHANNELS_FILE    Override channels file path (default: .helix/channels.yaml)
`

// printChannelHelp renders the channel subcommand help text.
func printChannelHelp(w io.Writer) {
	fmt.Fprint(w, channelHelp)
}

// ---------------------------------------------------------------------------
// Persistence (.helix/channels.yaml)
// ---------------------------------------------------------------------------

// channelFile is the persisted shape of .helix/channels.yaml. It carries
// the CLI agent identity (Ed25519 keypair) used to sign messages, the
// channel list, and the full message log.
type channelFile struct {
	Identity *channelFileIdentity `yaml:"identity"`
	Channels []channelYAML        `yaml:"channels"`
	Messages []messageYAML        `yaml:"messages"`
}

// channelFileIdentity is the persisted CLI agent identity. The Ed25519
// keys are stored as base64-encoded strings (yaml.v3 does not emit
// !!binary for []byte, so base64 is applied explicitly).
type channelFileIdentity struct {
	Name       string    `yaml:"name"`
	AgentID    string    `yaml:"agent_id"`
	PubKey     string    `yaml:"pubkey"`      // base64 ed25519 public key
	PrivateKey string    `yaml:"private_key"` // base64 ed25519 private key
	CreatedAt  time.Time `yaml:"created_at"`
}

// channelYAML mirrors channel.Channel with explicit YAML keys so the
// on-disk file stays readable.
type channelYAML struct {
	ID        string                `yaml:"id"`
	Name      string                `yaml:"name"`
	Type      channel.ChannelType   `yaml:"type"`
	Status    channel.ChannelStatus `yaml:"status"`
	Members   []string              `yaml:"members"`
	CreatedAt time.Time             `yaml:"created_at"`
	UpdatedAt time.Time             `yaml:"updated_at"`
}

// attachmentYAML mirrors channel.Attachment with base64-encoded data.
type attachmentYAML struct {
	Name        string `yaml:"name"`
	ContentType string `yaml:"content_type"`
	Data        string `yaml:"data"` // base64
}

// hidProofYAML mirrors channel.HIDSignature with base64-encoded sig bytes.
type hidProofYAML struct {
	KeyID       string `yaml:"key_id"`
	SigBytes    string `yaml:"sig_bytes"` // base64
	Fingerprint string `yaml:"fingerprint"`
}

// messageYAML mirrors channel.ChannelMessage with explicit YAML keys.
type messageYAML struct {
	ID           string              `yaml:"id"`
	ChannelID    string              `yaml:"channel_id"`
	Author       string              `yaml:"author"`
	AuthorType   channel.AuthorType  `yaml:"author_type"`
	Type         channel.MessageType `yaml:"type"`
	Content      string              `yaml:"content"`
	Attachments  []attachmentYAML    `yaml:"attachments,omitempty"`
	HIDProof     *hidProofYAML       `yaml:"hid_proof,omitempty"`
	ChimeraTrace any                 `yaml:"chimera_trace,omitempty"`
	Timestamp    time.Time           `yaml:"timestamp"`
}

func toChannelYAML(ch *channel.Channel) channelYAML {
	return channelYAML{
		ID:        ch.ID,
		Name:      ch.Name,
		Type:      ch.Type,
		Status:    ch.Status,
		Members:   append([]string(nil), ch.Members...),
		CreatedAt: ch.CreatedAt,
		UpdatedAt: ch.UpdatedAt,
	}
}

func (cy channelYAML) toChannel() *channel.Channel {
	return &channel.Channel{
		ID:        cy.ID,
		Name:      cy.Name,
		Type:      cy.Type,
		Status:    cy.Status,
		Members:   append([]string(nil), cy.Members...),
		CreatedAt: cy.CreatedAt,
		UpdatedAt: cy.UpdatedAt,
	}
}

func toMessageYAML(m *channel.ChannelMessage) messageYAML {
	my := messageYAML{
		ID:           m.ID,
		ChannelID:    m.ChannelID,
		Author:       m.Author,
		AuthorType:   m.AuthorType,
		Type:         m.Type,
		Content:      m.Content,
		ChimeraTrace: m.ChimeraTrace,
		Timestamp:    m.Timestamp,
	}
	for _, a := range m.Attachments {
		my.Attachments = append(my.Attachments, attachmentYAML{
			Name:        a.Name,
			ContentType: a.ContentType,
			Data:        base64.StdEncoding.EncodeToString(a.Data),
		})
	}
	if m.HIDProof != nil {
		my.HIDProof = &hidProofYAML{
			KeyID:       m.HIDProof.KeyID,
			SigBytes:    base64.StdEncoding.EncodeToString(m.HIDProof.SigBytes),
			Fingerprint: m.HIDProof.Fingerprint,
		}
	}
	return my
}

// toMessage converts a stored message back to channel.ChannelMessage,
// decoding the base64 attachment and signature payloads. Corrupt base64
// is reported as an error rather than silently producing a message whose
// signature can never verify.
func (my messageYAML) toMessage() (*channel.ChannelMessage, error) {
	m := &channel.ChannelMessage{
		ID:           my.ID,
		ChannelID:    my.ChannelID,
		Author:       my.Author,
		AuthorType:   my.AuthorType,
		Type:         my.Type,
		Content:      my.Content,
		ChimeraTrace: my.ChimeraTrace,
		Timestamp:    my.Timestamp,
	}
	for _, a := range my.Attachments {
		data, err := base64.StdEncoding.DecodeString(a.Data)
		if err != nil {
			return nil, fmt.Errorf("message %q attachment %q is not valid base64: %w", my.ID, a.Name, err)
		}
		m.Attachments = append(m.Attachments, channel.Attachment{
			Name:        a.Name,
			ContentType: a.ContentType,
			Data:        data,
		})
	}
	if my.HIDProof != nil {
		sig, err := base64.StdEncoding.DecodeString(my.HIDProof.SigBytes)
		if err != nil {
			return nil, fmt.Errorf("message %q hid_proof is not valid base64: %w", my.ID, err)
		}
		m.HIDProof = &channel.HIDSignature{
			KeyID:       my.HIDProof.KeyID,
			SigBytes:    sig,
			Fingerprint: my.HIDProof.Fingerprint,
		}
	}
	return m, nil
}

// channelsFilePath resolves the channels YAML path: HELIX_CHANNELS_FILE
// when set, otherwise .helix/channels.yaml relative to the current
// directory.
func channelsFilePath() string {
	if p := os.Getenv(envChannelFile); p != "" {
		return p
	}
	return filepath.Join(".helix", "channels.yaml")
}

// loadChannelFile reads the channels file. A missing file is treated as an
// empty channelFile (no channels, no messages, no identity yet).
func loadChannelFile(path string) (*channelFile, error) {
	file := &channelFile{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, file); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return file, nil
}

// saveChannelFile marshals and writes the channels file, creating the
// parent directory if needed.
func saveChannelFile(path string, file *channelFile) error {
	data, err := yaml.Marshal(file)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// findChannelByName returns a copy of the channel with the given name, or
// nil if it does not exist.
func findChannelByName(file *channelFile, name string) *channel.Channel {
	for _, cy := range file.Channels {
		if cy.Name == name {
			return cy.toChannel()
		}
	}
	return nil
}

// updateChannelByName replaces the stored entry for ch.Name with ch.
func updateChannelByName(file *channelFile, ch *channel.Channel) {
	for i := range file.Channels {
		if file.Channels[i].Name == ch.Name {
			file.Channels[i] = toChannelYAML(ch)
			return
		}
	}
}

// splitMembers parses a comma-separated member list, trimming whitespace
// and dropping empty entries.
func splitMembers(list string) []string {
	var out []string
	for _, m := range strings.Split(list, ",") {
		if m = strings.TrimSpace(m); m != "" {
			out = append(out, m)
		}
	}
	return out
}

// ensureChannelIdentity returns the CLI agent identity stored in file,
// creating a fresh one (identity.NewAgentIdentity) if absent. The caller
// must save the file afterwards for the identity to persist. The returned
// private key signs outgoing messages; the identity verifies them during
// archival.
func ensureChannelIdentity(file *channelFile) (*identity.AgentIdentity, ed25519.PrivateKey, error) {
	if file.Identity == nil {
		id, priv, err := identity.NewAgentIdentity(cliIdentityName)
		if err != nil {
			return nil, nil, fmt.Errorf("create identity: %w", err)
		}
		file.Identity = &channelFileIdentity{
			Name:       cliIdentityName,
			AgentID:    id.ID,
			PubKey:     base64.StdEncoding.EncodeToString(id.PubKey),
			PrivateKey: base64.StdEncoding.EncodeToString(priv),
			CreatedAt:  time.Now().UTC(),
		}
	}
	fi := file.Identity
	pub, err := base64.StdEncoding.DecodeString(fi.PubKey)
	if err != nil {
		return nil, nil, fmt.Errorf("stored identity pubkey is not valid base64: %w", err)
	}
	priv, err := base64.StdEncoding.DecodeString(fi.PrivateKey)
	if err != nil {
		return nil, nil, fmt.Errorf("stored identity private key is not valid base64: %w", err)
	}
	if len(pub) != ed25519.PublicKeySize {
		return nil, nil, fmt.Errorf("stored identity has invalid public key size %d", len(pub))
	}
	if len(priv) != ed25519.PrivateKeySize {
		return nil, nil, fmt.Errorf("stored identity has invalid private key size %d", len(priv))
	}
	return &identity.AgentIdentity{ID: fi.AgentID, PubKey: pub}, ed25519.PrivateKey(priv), nil
}

// ---------------------------------------------------------------------------
// Dispatcher
// ---------------------------------------------------------------------------

// runChannel dispatches to the requested channel subcommand.
func runChannel(args []string, stdout, stderr io.Writer) int {
	flags, helpWanted, exitCode := parseChannelFlags(args)
	if exitCode != channelExitOK {
		fmt.Fprintln(stderr, "channel: invalid arguments")
		printChannelHelp(stderr)
		return channelExitError
	}
	if helpWanted || flags.subcommand == "" {
		printChannelHelp(stdout)
		return channelExitOK
	}

	switch flags.subcommand {
	case "create":
		return runChannelCreate(flags, stdout, stderr)
	case "join":
		return runChannelJoin(flags, stdout, stderr)
	case "send":
		return runChannelSend(flags, stdout, stderr)
	case "list":
		return runChannelList(flags, stdout, stderr)
	case "archive":
		return runChannelArchive(flags, stdout, stderr)
	case "history":
		return runChannelHistory(flags, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "channel: unknown subcommand %q\n\n", flags.subcommand)
		printChannelHelp(stderr)
		return channelExitError
	}
}

// runChannelWithDryRun threads the global --dry-run flag through to the
// channel subcommands and maps non-zero exit codes to errExit so main.go
// propagates the documented exit-code contract.
func runChannelWithDryRun(args []string, stdout, stderr io.Writer, dryRun bool) error {
	if dryRun {
		args = append(append([]string{}, args...), "--dry-run")
	}
	rc := runChannel(args, stdout, stderr)
	if rc != channelExitOK {
		return errExit{code: rc}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Subcommands
// ---------------------------------------------------------------------------

// runChannelCreate creates a channel in .helix/channels.yaml. The channel
// is validated (name non-empty, type recognised) before anything is
// written; a duplicate name is rejected.
func runChannelCreate(f channelFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "channel create: --name is required")
		printChannelHelp(stderr)
		return channelExitError
	}
	if f.typ == "" {
		fmt.Fprintln(stderr, "channel create: --type is required (task, review, deliberation, incident)")
		printChannelHelp(stderr)
		return channelExitError
	}
	ctype := channel.ChannelType(f.typ)
	if !ctype.Valid() {
		fmt.Fprintf(stderr, "channel create: invalid type %q (task, review, deliberation, incident)\n", f.typ)
		return channelExitError
	}
	members := splitMembers(f.members)

	path := channelsFilePath()
	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] would create channel %q (type: %s, members: %s) in %s\n",
			f.name, ctype, strings.Join(members, ","), path)
		return channelExitOK
	}

	file, err := loadChannelFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "channel create: %v\n", err)
		return channelExitError
	}
	if findChannelByName(file, f.name) != nil {
		fmt.Fprintf(stderr, "channel create: channel %q already exists\n", f.name)
		return channelExitError
	}

	ch := channel.NewChannel(f.name, ctype, members)
	file.Channels = append(file.Channels, toChannelYAML(ch))
	if err := saveChannelFile(path, file); err != nil {
		fmt.Fprintf(stderr, "channel create: %v\n", err)
		return channelExitError
	}

	fmt.Fprintf(stdout, "✓ channel %q created (type: %s, members: %s)\n",
		f.name, ctype, strings.Join(members, ","))
	return channelExitOK
}

// runChannelJoin adds the CLI agent identity to a channel's members. The
// join is idempotent: an identity that is already a member exits 0 without
// modifying the channel.
func runChannelJoin(f channelFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "channel join: --name is required")
		printChannelHelp(stderr)
		return channelExitError
	}

	path := channelsFilePath()
	file, err := loadChannelFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "channel join: %v\n", err)
		return channelExitError
	}
	ch := findChannelByName(file, f.name)
	if ch == nil {
		fmt.Fprintf(stderr, "channel join: channel %q not found\n", f.name)
		return channelExitError
	}
	if ch.IsArchived() {
		fmt.Fprintf(stderr, "channel join: channel %q is archived\n", f.name)
		return channelExitError
	}

	if _, _, err := ensureChannelIdentity(file); err != nil {
		fmt.Fprintf(stderr, "channel join: %v\n", err)
		return channelExitError
	}

	if ch.HasMember(cliIdentityName) {
		fmt.Fprintf(stdout, "✓ already a member of %q\n", f.name)
		return channelExitOK
	}
	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] would join channel %q as %q\n", f.name, cliIdentityName)
		return channelExitOK
	}

	ch.Members = append(ch.Members, cliIdentityName)
	ch.UpdatedAt = time.Now().UTC()
	updateChannelByName(file, ch)
	if err := saveChannelFile(path, file); err != nil {
		fmt.Fprintf(stderr, "channel join: %v\n", err)
		return channelExitError
	}

	fmt.Fprintf(stdout, "✓ joined channel %q as %q\n", f.name, cliIdentityName)
	return channelExitOK
}

// runChannelSend appends a signed text message to a channel. Sending to a
// non-existent or archived channel is rejected. An optional --attachment
// file is embedded verbatim in the message.
func runChannelSend(f channelFlags, stdout, stderr io.Writer) int {
	if f.channel == "" {
		fmt.Fprintln(stderr, "channel send: --channel is required")
		printChannelHelp(stderr)
		return channelExitError
	}
	if f.message == "" {
		fmt.Fprintln(stderr, "channel send: --message is required")
		printChannelHelp(stderr)
		return channelExitError
	}

	path := channelsFilePath()
	file, err := loadChannelFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "channel send: %v\n", err)
		return channelExitError
	}
	ch := findChannelByName(file, f.channel)
	if ch == nil {
		fmt.Fprintf(stderr, "channel send: channel %q not found\n", f.channel)
		return channelExitError
	}
	if ch.IsArchived() {
		fmt.Fprintf(stderr, "channel send: channel %q is archived\n", f.channel)
		return channelExitError
	}

	var attachment *channel.Attachment
	if f.attachment != "" {
		data, err := os.ReadFile(f.attachment)
		if err != nil {
			fmt.Fprintf(stderr, "channel send: read attachment %s: %v\n", f.attachment, err)
			return channelExitError
		}
		attachment = &channel.Attachment{Name: filepath.Base(f.attachment), Data: data}
	}

	if f.dryRun {
		fmt.Fprintf(stdout, "[DRY-RUN] would send message to %q (author: %s, attachment: %s)\n",
			f.channel, cliIdentityName, f.attachment)
		return channelExitOK
	}

	id, priv, err := ensureChannelIdentity(file)
	if err != nil {
		fmt.Fprintf(stderr, "channel send: %v\n", err)
		return channelExitError
	}

	msg := channel.NewChannelMessage(ch.ID, cliIdentityName, channel.AuthorAgent, channel.MsgText, f.message)
	if attachment != nil {
		msg.Attachments = []channel.Attachment{*attachment}
	}
	if err := channel.SignMessage(msg, id, priv); err != nil {
		fmt.Fprintf(stderr, "channel send: sign: %v\n", err)
		return channelExitError
	}

	file.Messages = append(file.Messages, toMessageYAML(msg))
	if err := saveChannelFile(path, file); err != nil {
		fmt.Fprintf(stderr, "channel send: %v\n", err)
		return channelExitError
	}

	fmt.Fprintf(stdout, "✓ message sent to %q (id: %s, author: %s)\n", f.channel, msg.ID, cliIdentityName)
	return channelExitOK
}

// runChannelList prints channels as a table sorted by name, optionally
// filtered by --status.
func runChannelList(f channelFlags, stdout, stderr io.Writer) int {
	var status *channel.ChannelStatus
	if f.status != "" {
		s := channel.ChannelStatus(f.status)
		if s != channel.ChannelStatusActive && s != channel.ChannelStatusArchived {
			fmt.Fprintf(stderr, "channel list: invalid status %q (active, archived)\n", f.status)
			return channelExitError
		}
		status = &s
	}

	file, err := loadChannelFile(channelsFilePath())
	if err != nil {
		fmt.Fprintf(stderr, "channel list: %v\n", err)
		return channelExitError
	}

	chs := make([]*channel.Channel, 0, len(file.Channels))
	for _, cy := range file.Channels {
		ch := cy.toChannel()
		if status == nil || ch.Status == *status {
			chs = append(chs, ch)
		}
	}
	if len(chs) == 0 {
		fmt.Fprintln(stdout, "no channels")
		return channelExitOK
	}

	sort.Slice(chs, func(i, j int) bool { return chs[i].Name < chs[j].Name })
	fmt.Fprintf(stdout, "%-20s %-14s %-9s %s\n", "NAME", "TYPE", "STATUS", "MEMBERS")
	for _, ch := range chs {
		fmt.Fprintf(stdout, "%-20s %-14s %-9s %s\n",
			ch.Name, ch.Type, ch.Status, strings.Join(ch.Members, ","))
	}
	return channelExitOK
}

// runChannelArchive verifies every message's HID proof via
// channel.ArchiveChannel (fail closed) and marks the channel archived in
// the YAML. Re-archiving an already-archived channel re-verifies and
// reports without touching state.
func runChannelArchive(f channelFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "channel archive: --name is required")
		printChannelHelp(stderr)
		return channelExitError
	}

	path := channelsFilePath()
	file, err := loadChannelFile(path)
	if err != nil {
		fmt.Fprintf(stderr, "channel archive: %v\n", err)
		return channelExitError
	}
	ch := findChannelByName(file, f.name)
	if ch == nil {
		fmt.Fprintf(stderr, "channel archive: channel %q not found\n", f.name)
		return channelExitError
	}

	id, _, err := ensureChannelIdentity(file)
	if err != nil {
		fmt.Fprintf(stderr, "channel archive: %v\n", err)
		return channelExitError
	}

	msgs := make([]channel.ChannelMessage, 0, len(file.Messages))
	for _, my := range file.Messages {
		if my.ChannelID == ch.ID {
			m, err := my.toMessage()
			if err != nil {
				fmt.Fprintf(stderr, "channel archive: %v\n", err)
				return channelExitError
			}
			msgs = append(msgs, *m)
		}
	}

	// Fail closed: a single unsigned or tampered message aborts the
	// archival and leaves the channel state untouched.
	res, err := channel.ArchiveChannel(ch, msgs, memory.NewMemStore(), id)
	if err != nil {
		fmt.Fprintf(stderr, "channel archive: %v\n", err)
		return channelExitError
	}

	alreadyArchived := ch.IsArchived()
	if !alreadyArchived {
		ch.Status = channel.ChannelStatusArchived
		ch.UpdatedAt = time.Now().UTC()
		updateChannelByName(file, ch)
		if err := saveChannelFile(path, file); err != nil {
			fmt.Fprintf(stderr, "channel archive: %v\n", err)
			return channelExitError
		}
	}

	if alreadyArchived {
		fmt.Fprintf(stdout, "%q is already archived — re-verified %d message(s): %d written, %d skipped (idempotent)\n",
			f.name, len(msgs), res.Written, res.Skipped)
	} else {
		fmt.Fprintf(stdout, "✓ archived %q: %d written, %d skipped (idempotent)\n",
			f.name, res.Written, res.Skipped)
	}
	return channelExitOK
}

// runChannelHistory prints a channel's messages sorted by timestamp
// ascending, truncated to the most-recent --limit (default 20).
func runChannelHistory(f channelFlags, stdout, stderr io.Writer) int {
	if f.name == "" {
		fmt.Fprintln(stderr, "channel history: --name is required")
		printChannelHelp(stderr)
		return channelExitError
	}

	file, err := loadChannelFile(channelsFilePath())
	if err != nil {
		fmt.Fprintf(stderr, "channel history: %v\n", err)
		return channelExitError
	}
	ch := findChannelByName(file, f.name)
	if ch == nil {
		fmt.Fprintf(stderr, "channel history: channel %q not found\n", f.name)
		return channelExitError
	}

	msgs := make([]channel.ChannelMessage, 0, len(file.Messages))
	for _, my := range file.Messages {
		if my.ChannelID == ch.ID {
			m, err := my.toMessage()
			if err != nil {
				fmt.Fprintf(stderr, "channel history: %v\n", err)
				return channelExitError
			}
			msgs = append(msgs, *m)
		}
	}
	if len(msgs) == 0 {
		fmt.Fprintf(stdout, "no messages in channel %q\n", f.name)
		return channelExitOK
	}

	sort.SliceStable(msgs, func(i, j int) bool { return msgs[i].Timestamp.Before(msgs[j].Timestamp) })
	limit := f.limit
	if limit <= 0 {
		limit = channelHistoryDefaultLimit
	}
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}

	for _, m := range msgs {
		fmt.Fprintf(stdout, "%s  %s  %s  %s\n",
			m.ID, m.Author, m.Timestamp.UTC().Format(time.RFC3339), m.Content)
	}
	return channelExitOK
}
