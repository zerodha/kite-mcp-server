package mcp

import (
	"context"
	"fmt"
	"slices"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type listSkillsParams struct {
	sdk.ParamsBase
	Cursor string `json:"cursor,omitempty"`
}

type listSkillsResult struct {
	sdk.ResultBase
	ResultType string       `json:"resultType"`
	Skills     []skillEntry `json:"skills"`
	TTLMS      int          `json:"ttlMs"`
	CacheScope string       `json:"cacheScope"`
}

type getSkillParams struct {
	sdk.ParamsBase
	URI string `json:"uri"`
}

type getSkillResult struct {
	sdk.ResultBase
	ResultType string     `json:"resultType"`
	Skill      skillEntry `json:"skill"`
	TTLMS      int        `json:"ttlMs"`
	CacheScope string     `json:"cacheScope"`
}

func invalidSkillParams(message string) error {
	return &jsonrpc.Error{Code: -32602, Message: message}
}

// registerSkills uses the SDK's extension methods, retaining normal transport,
// authentication, request lifecycle, and middleware behavior.
func registerSkills(server *sdk.Server) {
	for uri, p := range workflowHelp.files {
		if p.key == "/" {
			continue
		} // The help index is not part of an Agent Skill.
		server.AddResource(&sdk.Resource{URI: uri, Name: p.file, MIMEType: "text/markdown"}, func(_ context.Context, _ *sdk.ReadResourceRequest) (*sdk.ReadResourceResult, error) {
			return &sdk.ReadResourceResult{Contents: []*sdk.ResourceContents{{URI: uri, MIMEType: "text/markdown", Text: p.text}}}, nil
		})
	}
	if err := sdk.AddReceivingCustomMethod(server, "skills/list", func(_ context.Context, _ *sdk.ServerSession, p *listSkillsParams) (*listSkillsResult, error) {
		// This small, static catalog is a single complete page. Never silently
		// interpret an arbitrary cursor as page zero.
		if p != nil && p.Cursor != "" {
			return nil, invalidSkillParams("Unknown skills cursor; omit cursor to start.")
		}
		return &listSkillsResult{ResultType: "complete", Skills: workflowHelp.skills, TTLMS: 300000, CacheScope: "public"}, nil
	}); err != nil {
		panic(fmt.Sprintf("register skills/list: %v", err))
	}
	if err := sdk.AddReceivingCustomMethod(server, "skills/get", func(_ context.Context, _ *sdk.ServerSession, p *getSkillParams) (*getSkillResult, error) {
		if p != nil {
			for _, skill := range workflowHelp.skills {
				if skill.URI == p.URI {
					return &getSkillResult{ResultType: "complete", Skill: skill, TTLMS: 300000, CacheScope: "public"}, nil
				}
			}
		}
		return nil, invalidSkillParams("Unknown skill URI; use the SKILL.md URI returned by skills/list.")
	}); err != nil {
		panic(fmt.Sprintf("register skills/get: %v", err))
	}
	server.AddReceivingMiddleware(func(next sdk.MethodHandler) sdk.MethodHandler {
		return func(ctx context.Context, method string, req sdk.Request) (sdk.Result, error) {
			// Read the negotiated session, never a process-global client flag. This
			// server supports pre-2026 protocols, where capabilities live in initialize.
			supported := false
			if ss, ok := req.GetSession().(*sdk.ServerSession); ok && ss != nil {
				if p := ss.InitializeParams(); p != nil && p.Capabilities != nil {
					_, supported = p.Capabilities.Extensions[skillsExtension].(map[string]any)
				}
			}
			if supported && method == "tools/call" {
				if p, ok := req.GetParams().(*sdk.CallToolParamsRaw); ok && p.Name == "help" {
					return NewToolResultError("Use skills/list, skills/get, and resources/read for this client's negotiated Skills interface."), nil
				}
			}
			result, err := next(ctx, method, req)
			if supported && method == "tools/list" && err == nil {
				if list, ok := result.(*sdk.ListToolsResult); ok {
					// Do not mutate SDK-owned/shared slices or another session's listing.
					copy := *list
					copy.Tools = slices.DeleteFunc(slices.Clone(list.Tools), func(t *sdk.Tool) bool { return t.Name == "help" })
					return &copy, nil
				}
			}
			return result, err
		}
	})
}
