package mcp

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/zerodha/kite-mcp-server/kc"
	"gopkg.in/yaml.v3"
)

const skillsExtension = "io.modelcontextprotocol/skills"

// Bound navigation work independently of the transport request-body limit.
const maxHelpKeyBytes = 1024

// The catalog is immutable for the lifetime of a binary. No request can read
// from the host filesystem, and both interfaces return the same source bytes.
//
//go:embed help
var helpFiles embed.FS

var skillNamePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

var workflowHelp = mustLoadHelp()

type helpPage struct {
	key, file, text string
	summary         string
}

type skillResource struct {
	URI    string `json:"uri"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

type skillEntry struct {
	URI         string          `json:"uri"`
	Frontmatter map[string]any  `json:"frontmatter"`
	Resources   []skillResource `json:"resources"`
}

type helpCatalog struct {
	pages  map[string]helpPage
	files  map[string]helpPage
	skills []skillEntry
}

func mustLoadHelp() *helpCatalog {
	c, err := loadHelp(helpFiles)
	if err != nil {
		panic(fmt.Sprintf("invalid embedded help: %v", err))
	}
	return c
}

func loadHelp(files fs.FS) (*helpCatalog, error) {
	c := &helpCatalog{pages: make(map[string]helpPage), files: make(map[string]helpPage)}
	frontmatter := make(map[string]map[string]any)
	err := fs.WalkDir(files, "help", func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		if path.Ext(file) != ".md" {
			return fmt.Errorf("unsupported help file %s", file)
		}
		b, err := fs.ReadFile(files, file)
		if err != nil {
			return err
		}
		rel := strings.TrimPrefix(file, "help/")
		key := "/" + strings.TrimSuffix(rel, ".md")
		if rel == "index.md" {
			key = "/"
		} else if path.Base(rel) == "SKILL.md" {
			key = "/" + path.Dir(rel)
		}
		if len(key) > maxHelpKeyBytes {
			return fmt.Errorf("help key exceeds %d bytes: %s", maxHelpKeyBytes, file)
		}
		if _, exists := c.pages[key]; exists {
			return fmt.Errorf("duplicate help key %s", key)
		}
		p := helpPage{key: key, file: rel, text: string(b)}
		front, body, err := parseHelpFrontmatter(p.text)
		if err != nil {
			return fmt.Errorf("%s: %w", file, err)
		}
		frontmatter[rel] = front
		// Recipes may supply a short description without becoming skills.
		p.summary, _ = front["description"].(string)
		if strings.TrimSpace(p.summary) == "" {
			for _, line := range strings.Split(body, "\n") {
				if strings.HasPrefix(line, "# ") {
					p.summary = strings.TrimSpace(strings.TrimPrefix(line, "# "))
					break
				}
			}
		}
		p.summary = strings.Join(strings.Fields(p.summary), " ")
		c.pages[key] = p
		c.files["skill://"+rel] = p
		return nil
	})
	if err != nil {
		return nil, err
	}
	if _, ok := c.pages["/"]; !ok {
		return nil, fmt.Errorf("missing root index.md")
	}
	for _, p := range c.pages {
		if path.Base(p.file) != "SKILL.md" {
			continue
		}
		front := frontmatter[p.file]
		if front == nil {
			return nil, fmt.Errorf("missing frontmatter in %s", p.file)
		}
		name, ok := front["name"].(string)
		description, _ := front["description"].(string)
		if !ok || name != path.Base(path.Dir(p.file)) || len(name) > 64 || !skillNamePattern.MatchString(name) || strings.TrimSpace(description) == "" || utf8.RuneCountInString(description) > 1024 {
			return nil, fmt.Errorf("invalid name or description in %s", p.file)
		}
		skill := skillEntry{URI: "skill://" + p.file, Frontmatter: front, Resources: []skillResource{}}
		prefix := "skill://" + path.Dir(p.file) + "/"
		totalBytes := 0
		for uri, file := range c.files {
			if strings.HasPrefix(uri, prefix) {
				totalBytes += len(file.text)
				digest := sha256.Sum256([]byte(file.text))
				skill.Resources = append(skill.Resources, skillResource{URI: uri, Digest: fmt.Sprintf("sha256:%x", digest), Size: len(file.text)})
			}
		}
		if len(skill.Resources) > 512 || totalBytes > 16*1024*1024 {
			return nil, fmt.Errorf("skill %s exceeds resource or byte limits", p.file)
		}
		sort.Slice(skill.Resources, func(i, j int) bool { return skill.Resources[i].URI < skill.Resources[j].URI })
		c.skills = append(c.skills, skill)
	}
	for _, p := range c.pages {
		if p.key == "/" {
			continue
		}
		covered := false
		for _, skill := range c.skills {
			if strings.HasPrefix("skill://"+p.file, strings.TrimSuffix(skill.URI, "SKILL.md")) {
				covered = true
				break
			}
		}
		if !covered {
			return nil, fmt.Errorf("help file %s has no enclosing SKILL.md", p.file)
		}
	}
	// Supporting directories need navigable nodes even without SKILL.md.
	// Keep these synthetic pages out of resource manifests and resource reads.
	keys := make([]string, 0, len(c.pages))
	for key := range c.pages {
		keys = append(keys, key)
	}
	for _, key := range keys {
		for parent := path.Dir(key); parent != "/"; parent = path.Dir(parent) {
			if _, exists := c.pages[parent]; exists {
				continue
			}
			summary := "Browse workflow documentation under " + parent + "."
			c.pages[parent] = helpPage{key: parent, text: "# " + path.Base(parent) + "\n\n" + summary + "\n", summary: summary}
		}
	}

	sort.Slice(c.skills, func(i, j int) bool { return c.skills[i].URI < c.skills[j].URI })
	return c, nil
}

// Parse a normalized copy; resource content and digests retain original bytes.
// Delimiters must occupy a whole line, including inside YAML block scalars.
func parseHelpFrontmatter(text string) (map[string]any, string, error) {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	rest, present := strings.CutPrefix(normalized, "---\n")
	if !present {
		return nil, normalized, nil
	}
	lines := strings.Split(rest, "\n")
	for i, line := range lines {
		if line != "---" {
			continue
		}
		front := make(map[string]any)
		if err := yaml.Unmarshal([]byte(strings.Join(lines[:i], "\n")+"\n"), &front); err != nil {
			return nil, "", err
		}
		return front, strings.Join(lines[i+1:], "\n"), nil
	}
	return nil, "", fmt.Errorf("missing frontmatter delimiter")
}

// HelpTool provides progressive disclosure for clients without Skills support.
type HelpTool struct{}

func (*HelpTool) Definition() *sdk.Tool {
	t := NewTool("help", "Browse Kite workflows and recipes as a tree. Start with key=/, then /orders, /holdings, /technical-analysis, /daily-review, or /troubleshooting. Follow child keys for focused recipes. Reads bundled Markdown only; does not execute trades or fetch account data.", json.RawMessage(`{"type":"object","properties":{"key":{"type":"string","maxLength":1024,"default":"/","description":"Exact help path (maximum 1024 UTF-8 bytes), e.g. /orders/reconcile. Omit for the root index."}},"additionalProperties":false}`))
	closed := false
	t.Annotations = &sdk.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true, OpenWorldHint: &closed}
	return t
}

func (*HelpTool) Handler(_ *kc.Manager) ToolHandler {
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		key := "/"
		if req == nil || req.Params == nil {
			return NewToolResultError("Expected an object with an optional string key; start at /."), nil
		}
		if len(req.Params.Arguments) > 0 {
			var args map[string]json.RawMessage
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil || args == nil {
				return NewToolResultError("Expected an object with an optional string key; start at /."), nil
			}
			for name, raw := range args {
				if name != "key" {
					return NewToolResultError("Unknown argument: " + name + ". Use key=/ to start."), nil
				}
				if string(raw) == "null" || json.Unmarshal(raw, &key) != nil {
					return NewToolResultError("key must be a string; start at /."), nil
				}
			}
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return workflowHelp.lookup(key), nil
	}
}

func (c *helpCatalog) lookup(key string) *sdk.CallToolResult {
	if len(key) > maxHelpKeyBytes {
		return NewToolResultError(fmt.Sprintf("Help key exceeds %d UTF-8 bytes. Start at `/` and follow the listed child keys.", maxHelpKeyBytes))
	}
	if key == "" {
		key = "/"
	}
	if key != "/" {
		key = strings.TrimSuffix(key, "/")
	}
	p, ok := c.pages[key]
	if !ok {
		// Check known paths rather than repeatedly hashing every ancestor
		// of untrusted input. Work is bounded by the embedded catalog size.
		ancestor := "/"
		for candidate := range c.pages {
			if len(candidate) > len(ancestor) && strings.HasPrefix(key, candidate+"/") {
				ancestor = candidate
			}
		}
		return NewToolResultError("Unknown help key. Keys are exact and case-sensitive. Browse `" + ancestor + "` or return to `/`." + c.childKeys(ancestor))
	}
	text := p.text
	if len(c.children(key)) > 0 {
		title := path.Base(key)
		if key == "/" {
			title = "Kite workflow help"
		}
		text = "# " + title + "\n\n" + p.summary + "\n\nChoose a child key to continue. Categories contain more choices; recipes contain full workflow steps.\n" + c.childKeys(key)
	}
	if key != "/" {
		text += "\nParent key: `" + path.Dir(key) + "`\n"
	}
	return NewToolResultText(text)
}

func (c *helpCatalog) children(key string) []string {
	children := []string{}
	for child := range c.pages {
		if child != key && path.Dir(child) == key {
			children = append(children, child)
		}
	}
	sort.Strings(children)
	return children
}

// childKeys shows only immediate choices, keeping discovery bounded and useful.
func (c *helpCatalog) childKeys(key string) string {
	children := c.children(key)
	if len(children) == 0 {
		return ""
	}
	var text strings.Builder
	text.WriteString("\n## Child keys\n")
	for _, child := range children {
		kind := "recipe"
		if len(c.children(child)) > 0 {
			kind = "category"
		}
		fmt.Fprintf(&text, "\n- `%s` — %s (%s)", child, c.pages[child].summary, kind)
	}
	text.WriteString("\n")
	return text.String()
}
