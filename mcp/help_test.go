package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestHelpTree(t *testing.T) {
	h := (&HelpTool{}).Handler(nil)
	for _, key := range []string{"/", "/orders", "/orders/", "/orders/reconcile", "/holdings/concentration", "/technical-analysis/rsi", "/daily-review"} {
		t.Run(key, func(t *testing.T) {
			args, _ := json.Marshal(map[string]any{"key": key})
			result, err := h(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: args}})
			require.NoError(t, err)
			require.False(t, result.IsError)
			require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "#")
			if key == "/" {
				require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "`/technical-analysis`")
			}
			if key == "/orders" {
				require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "`/orders/reconcile`")
			}
		})
	}
	for _, raw := range []string{`{"key":"../orders"}`, `{"key":"/orders/../holdings"}`, `{"key":"/ORDERS"}`, `{"key":"/orders/*"}`, `{"key":"/orders%2freconcile"}`, `{"key":null}`, `{"key":5}`, `{"key":[]}`, `null`, `[]`, `{"wat":1}`} {
		r, err := h(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(raw)}})
		require.NoError(t, err)
		require.True(t, r.IsError, raw)
	}
	r, err := h(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(`{}`)}})
	require.NoError(t, err)
	require.False(t, r.IsError)
	require.Contains(t, r.Content[0].(*sdk.TextContent).Text, "Child keys")
}

func TestCatalogLinksAndIntegrity(t *testing.T) {
	links := regexp.MustCompile(`\]\(([^)]+\.md)\)`)
	for _, p := range workflowHelp.pages {
		for _, match := range links.FindAllStringSubmatch(p.text, -1) {
			target := "skill://" + path.Join(path.Dir(p.file), match[1])
			_, exists := workflowHelp.files[target]
			require.True(t, exists, "broken link from %s: %s", p.file, target)
		}
	}
	for _, skill := range workflowHelp.skills {
		require.Equal(t, path.Base(path.Dir(strings.TrimPrefix(skill.URI, "skill://"))), skill.Frontmatter["name"])
		found := false
		for _, r := range skill.Resources {
			p, ok := workflowHelp.files[r.URI]
			require.True(t, ok)
			require.Equal(t, len(p.text), r.Size)
			require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(p.text))), r.Digest)
			found = found || r.URI == skill.URI
		}
		require.True(t, found)
	}
}

func TestInvalidHelpCatalog(t *testing.T) {
	for _, files := range []fstest.MapFS{
		{"help/orders/SKILL.md": {Data: []byte("missing frontmatter")}},
		{"help/index.md": {}, "help/orders/SKILL.md": {Data: []byte("---\nname: wrong\ndescription: invalid\n---\n")}},
		{"help/index.md": {}, "help/orders/SKILL.md": {Data: []byte("---\nname: orders\n---\n")}},
		{"help/index.md": {}, "help/secret.txt": {}},
	} {
		_, err := loadHelp(files)
		require.Error(t, err)
	}
}

func TestSkillsAndLegacySessions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	server := NewServer(&sdk.Implementation{Name: "help-test", Version: "1"})
	RegisterTools(server, nil, "", slog.New(slog.NewTextHandler(io.Discard, nil)))
	type peer struct {
		session *sdk.ClientSession
		skills  bool
	}
	var peers []peer
	// Keep differently negotiated clients connected to the same server at once.
	for _, supports := range []bool{true, false, true, false} {
		options := &sdk.ClientOptions{Capabilities: &sdk.ClientCapabilities{}}
		if supports {
			options.Capabilities.AddExtension(skillsExtension, nil)
		}
		client := sdk.NewClient(&sdk.Implementation{Name: "test", Version: "1"}, options)
		require.NoError(t, sdk.AddSendingCustomMethod[*listSkillsParams, *listSkillsResult](client, "skills/list"))
		require.NoError(t, sdk.AddSendingCustomMethod[*getSkillParams, *getSkillResult](client, "skills/get"))
		ct, st := sdk.NewInMemoryTransports()
		ss, err := server.Connect(ctx, st, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, ss.Close()) }()
		cs, err := client.Connect(ctx, ct, nil)
		require.NoError(t, err)
		defer func() { require.NoError(t, cs.Close()) }()
		peers = append(peers, peer{cs, supports})
		caps := cs.InitializeResult().Capabilities
		require.Contains(t, caps.Extensions, skillsExtension)
		require.NotNil(t, caps.Resources)
		tools, err := cs.ListTools(ctx, nil)
		require.NoError(t, err)
		found := false
		for _, tool := range tools.Tools {
			found = found || tool.Name == "help"
		}
		require.Equal(t, !supports, found)
		help, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: "help", Arguments: map[string]any{"key": "/orders/reconcile"}})
		require.NoError(t, err)
		require.Equal(t, supports, help.IsError)
		listed, err := sdk.CallCustomMethod[*listSkillsParams, *listSkillsResult](ctx, cs, "skills/list", &listSkillsParams{})
		require.NoError(t, err)
		require.Equal(t, "complete", listed.ResultType)
		require.Len(t, listed.Skills, len(workflowHelp.skills))
		require.Equal(t, "public", listed.CacheScope)
		require.Positive(t, listed.TTLMS)
		for _, skill := range listed.Skills {
			got, err := sdk.CallCustomMethod[*getSkillParams, *getSkillResult](ctx, cs, "skills/get", &getSkillParams{URI: skill.URI})
			require.NoError(t, err)
			require.Equal(t, skill, got.Skill)
			for _, resource := range skill.Resources {
				read, err := cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: resource.URI})
				require.NoError(t, err)
				require.Len(t, read.Contents, 1)
				require.Equal(t, resource.URI, read.Contents[0].URI)
				require.Equal(t, workflowHelp.files[resource.URI].text, read.Contents[0].Text)
				require.Equal(t, resource.Digest, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(read.Contents[0].Text))))
			}
		}
		_, err = sdk.CallCustomMethod[*getSkillParams, *getSkillResult](ctx, cs, "skills/get", &getSkillParams{URI: "skill://orders/../holdings/SKILL.md"})
		require.Error(t, err)
		var rpcErr *jsonrpc.Error
		require.True(t, errors.As(err, &rpcErr))
		require.EqualValues(t, -32602, rpcErr.Code)
		_, err = sdk.CallCustomMethod[*listSkillsParams, *listSkillsResult](ctx, cs, "skills/list", &listSkillsParams{Cursor: "bad"})
		require.Error(t, err)
		require.True(t, errors.As(err, &rpcErr))
		require.EqualValues(t, -32602, rpcErr.Code)
		_, err = cs.ReadResource(ctx, &sdk.ReadResourceParams{URI: "skill://orders/../../etc/passwd"})
		require.Error(t, err)
	}
	// Recheck earlier sessions after the last client has initialized. A global
	// negotiation flag would incorrectly change their tool lists.
	for _, p := range peers {
		listed, err := p.session.ListTools(ctx, nil)
		require.NoError(t, err)
		found := false
		for _, tool := range listed.Tools {
			found = found || tool.Name == "help"
		}
		require.Equal(t, !p.skills, found)
	}

}

func TestNestedSkillManifestAndFrontmatter(t *testing.T) {
	files := fstest.MapFS{
		"help/index.md":                     {Data: []byte("# Help\n")},
		"help/orders/SKILL.md":              {Data: []byte("---\nname: orders\ndescription: Order workflows\nlicense: MIT\nmetadata:\n  author: Kite\n---\n# Orders\n")},
		"help/orders/recovery/SKILL.md":     {Data: []byte("---\nname: recovery\ndescription: Recover uncertain orders\n---\n# Recovery\n")},
		"help/orders/recovery/reconcile.md": {Data: []byte("# Reconcile\n")},
	}
	c, err := loadHelp(files)
	require.NoError(t, err)
	require.Len(t, c.skills, 2)
	require.Equal(t, "MIT", c.skills[0].Frontmatter["license"])
	require.Equal(t, map[string]any{"author": "Kite"}, c.skills[0].Frontmatter["metadata"])
	// The enclosing skill must cover the nested skill's files too.
	require.Len(t, c.skills[0].Resources, 3)
	require.Len(t, c.skills[1].Resources, 2)
}

func TestHelpDiscoveryAndRecovery(t *testing.T) {
	h := (&HelpTool{}).Handler(nil)
	for _, tc := range []struct {
		key     string
		isError bool
		want    []string
	}{
		{"/", false, []string{"`/orders` — Inspect orders", "`/holdings` — Review holdings"}},
		{"/orders/", false, []string{"`/orders/reconcile` — Inspect order status, partial fills", "`/orders/prepare` — Resolve instruments"}},
		{"/orders/unknown", true, []string{"Browse `/orders`", "`/orders/reconcile` — Inspect order status"}},
		{"/orders/unknown/deeper", true, []string{"Browse `/orders`", "`/orders/prepare` — Resolve instruments"}},
		{"/unknown", true, []string{"Browse `/`", "`/orders` — Inspect orders"}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			args, err := json.Marshal(map[string]string{"key": tc.key})
			require.NoError(t, err)
			result, err := h(context.Background(), &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: args}})
			require.NoError(t, err)
			require.Equal(t, tc.isError, result.IsError)
			text := result.Content[0].(*sdk.TextContent).Text
			for _, want := range tc.want {
				require.Contains(t, text, want)
			}
			if tc.key == "/" {
				require.NotContains(t, text, "`/orders/reconcile`")
			}
		})
	}
}

func TestHelpCRLFAndRawResourceIntegrity(t *testing.T) {
	raw := "---\r\nname: orders\r\ndescription: Order workflows\r\nmetadata:\r\n  note: |\r\n    Keep this ---\r\n    inside the scalar.\r\n---\r\n# Orders\r\n"
	c, err := loadHelp(fstest.MapFS{
		"help/index.md":            {Data: []byte("# Help\r\n")},
		"help/orders/SKILL.md":     {Data: []byte(raw)},
		"help/orders/reconcile.md": {Data: []byte("---\r\ndescription: Inspect order fills.\r\n---\r\n# Reconcile\r\n")},
	})
	require.NoError(t, err)
	require.Equal(t, "Order workflows", c.pages["/orders"].summary)
	require.Equal(t, "Inspect order fills.", c.pages["/orders/reconcile"].summary)
	require.Equal(t, raw, c.files["skill://orders/SKILL.md"].text)
	require.Equal(t, "Keep this ---\ninside the scalar.\n", c.skills[0].Frontmatter["metadata"].(map[string]any)["note"])
	for _, r := range c.skills[0].Resources {
		if r.URI == "skill://orders/SKILL.md" {
			require.Equal(t, len(raw), r.Size)
			require.Equal(t, fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(raw))), r.Digest)
		}
	}
}

func TestHelpNestedDirectoriesAndProgressiveDisclosure(t *testing.T) {
	c, err := loadHelp(fstest.MapFS{
		"help/index.md":                             {Data: []byte("# Help\n")},
		"help/orders/SKILL.md":                      {Data: []byte("---\nname: orders\ndescription: Inspect orders\n---\n# Orders\nBRANCH DETAIL\n")},
		"help/orders/references/fills/reconcile.md": {Data: []byte("---\ndescription: Reconcile order fills\n---\n# Reconcile\nFULL RECIPE STEPS\n")},
	})
	require.NoError(t, err)
	for _, key := range []string{"/", "/orders", "/orders/references", "/orders/references/fills"} {
		result := c.lookup(key)
		require.False(t, result.IsError, key)
		text := result.Content[0].(*sdk.TextContent).Text
		require.Contains(t, text, "Child keys")
		require.NotContains(t, text, "BRANCH DETAIL")
		require.NotContains(t, text, "FULL RECIPE STEPS")
	}
	root := c.lookup("/").Content[0].(*sdk.TextContent).Text
	require.Contains(t, root, "`/orders` — Inspect orders (category)")
	require.NotContains(t, root, "`/orders/references`")
	branch := c.lookup("/orders/references/fills").Content[0].(*sdk.TextContent).Text
	require.Contains(t, branch, "Reconcile order fills (recipe)")
	leaf := c.lookup("/orders/references/fills/reconcile").Content[0].(*sdk.TextContent).Text
	require.Contains(t, leaf, "FULL RECIPE STEPS")
	require.Contains(t, leaf, "Parent key: `/orders/references/fills`")
	unknown := c.lookup("/orders/references/fills/unknown")
	require.True(t, unknown.IsError)
	require.Contains(t, unknown.Content[0].(*sdk.TextContent).Text, "Reconcile order fills (recipe)")
	// Virtual navigation pages must never be advertised as physical skill files.
	require.Len(t, c.files, 3)
	require.Len(t, c.skills[0].Resources, 2)
	require.NotContains(t, c.files, "skill://orders/references")
}

func TestHelpKeyBoundsAndRecovery(t *testing.T) {
	for _, key := range []string{
		"/" + strings.Repeat("x/", 500000),
		strings.Repeat("x", maxHelpKeyBytes+1),
		strings.Repeat("界", maxHelpKeyBytes/3+1),
	} {
		result := workflowHelp.lookup(key)
		require.True(t, result.IsError)
		require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "exceeds 1024 UTF-8 bytes")
	}
	// An at-limit key follows normal recovery; do not reject valid-size inputs.
	key := "/orders/" + strings.Repeat("x", maxHelpKeyBytes-len("/orders/"))
	result := workflowHelp.lookup(key)
	require.True(t, result.IsError)
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "Browse `/orders`")
	// Prefix matches must end at a path segment, not a similarly named category.
	result = workflowHelp.lookup("/orders-extra/unknown")
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "Browse `/`")
	// Deep, bounded input still finds the nearest category.
	result = workflowHelp.lookup("/orders/" + strings.Repeat("x/", 400) + "unknown")
	require.Contains(t, result.Content[0].(*sdk.TextContent).Text, "Browse `/orders`")
}

func TestHelpHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result, err := (&HelpTool{}).Handler(nil)(ctx, &sdk.CallToolRequest{Params: &sdk.CallToolParamsRaw{Arguments: json.RawMessage(`{"key":"/"}`)}})
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, result)
}

func BenchmarkHelpInvalidKey(b *testing.B) {
	for _, n := range []int{10000, 100000, 500000} {
		key := "/" + strings.Repeat("x/", n)
		b.Run(fmt.Sprintf("bytes=%d", len(key)), func(b *testing.B) {
			for b.Loop() {
				workflowHelp.lookup(key)
			}
		})
	}
}
