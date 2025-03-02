package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"text/template"

	"github.com/macrat/go-jsonrpc2"
)

var (
	Version = "UNKNOWN"
	Commit  = "HEAD"
)

type JsonMap map[string]any

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type InitializeResult struct {
	ProtocolVersion string     `json:"protocolVersion"`
	Capabilities    JsonMap    `json:"capabilities"`
	ServerInfo      ServerInfo `json:"serverInfo"`
	Instructions    string     `json:"instructions"`
}

type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
	Blob string `json:"blob,omitempty"`
}

func JSONContent(v any) (*Content, error) {
	bs, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return &Content{Type: "text", Text: string(bs)}, nil
}

type ToolInfo struct {
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	InputSchema JsonMap `json:"inputSchema"`
}

type ToolsListResult struct {
	Tools []ToolInfo `json:"tools"`
}

type ToolsCallRequest struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

type ToolsCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError"`
}

func UnmarshalParams[T any](data []byte, target *T) error {
	err := json.Unmarshal(data, target)
	if err != nil {
		return jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: fmt.Sprintf("Failed to parse parameters: %v", err),
		}
	}
	return nil
}

type KintoneAppDetail struct {
	AppID       string  `json:"appID"`
	Name        string  `json:"name"`
	Description string  `json:"description,omitempty"`
	Properties  JsonMap `json:"properties,omitempty"`
	CreatedAt   string  `json:"createdAt"`
	ModifiedAt  string  `json:"modifiedAt"`
}

type KintoneHandlers struct {
	URL   *url.URL
	Auth  string
	Token string
	Allow []string
	Deny  []string
}

func NewKintoneHandlersFromEnv() (*KintoneHandlers, error) {
	var handlers KintoneHandlers
	errs := []error{errors.New("Error:")}

	username := Getenv("KINTONE_USERNAME", "")
	password := Getenv("KINTONE_PASSWORD", "")
	tokens := Getenv("KINTONE_API_TOKEN", "")
	if (username == "" || password == "") && tokens == "" {
		errs = append(errs, errors.New("- Either KINTONE_USERNAME/KINTONE_PASSWORD or KINTONE_API_TOKEN must be provided"))
	}
	if username != "" && password != "" {
		handlers.Auth = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
	}
	handlers.Token = tokens

	baseURL := Getenv("KINTONE_BASE_URL", "")
	if baseURL == "" {
		errs = append(errs, errors.New("- KINTONE_BASE_URL must be provided"))
	} else if u, err := url.Parse(baseURL); err != nil {
		errs = append(errs, fmt.Errorf("- Failed to parse KINTONE_BASE_URL: %s", err))
	} else {
		handlers.URL = u
	}

	handlers.Allow = GetenvList("KINTONE_ALLOW_APPS")
	handlers.Deny = GetenvList("KINTONE_DENY_APPS")

	if len(errs) > 1 {
		return nil, errors.Join(errs...)
	}

	return &handlers, nil
}

type Query map[string]string

func (q Query) Encode() string {
	values := make(url.Values)
	for k, v := range q {
		values.Set(k, v)
	}
	return values.Encode()
}

func (h *KintoneHandlers) SendHTTP(ctx context.Context, method, path string, query Query, body any) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			return nil, jsonrpc2.Error{
				Code:    jsonrpc2.InternalErrorCode,
				Message: fmt.Sprintf("Failed to prepare request body for kintone server: %v", err),
			}
		}
		reqBody = bytes.NewReader(bs)
	}

	endpoint := h.URL.JoinPath(path)
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, method, endpoint.String(), reqBody)
	if err != nil {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: fmt.Sprintf("Failed to create HTTP request: %v", err),
		}
	}

	if h.Auth != "" {
		req.Header.Set("X-Cybozu-Authorization", h.Auth)
	}
	if h.Token != "" {
		req.Header.Set("X-Cybozu-API-Token", h.Token)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: fmt.Sprintf("Failed to send HTTP request to kintone server: %v", err),
		}
	}

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		res.Body.Close()
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: "kintone server returned an error",
			Data:    JsonMap{"status": res.Status, "message": string(msg)},
		}
	}

	return res, nil
}

func (h *KintoneHandlers) FetchHTTP(ctx context.Context, method, path string, query Query, body any, result any) error {
	res, err := h.SendHTTP(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if result != nil {
		if err := json.NewDecoder(res.Body).Decode(result); err != nil {
			return jsonrpc2.Error{
				Code:    jsonrpc2.InternalErrorCode,
				Message: fmt.Sprintf("Failed to parse kintone server's response: %v", err),
			}
		}
	}

	return nil
}

func (h *KintoneHandlers) InitializeHandler(ctx context.Context, params any) (InitializeResult, error) {
	return InitializeResult{
		ProtocolVersion: "2024-11-05",
		Capabilities: JsonMap{
			"tools": JsonMap{},
		},
		ServerInfo: ServerInfo{
			Name:    "Kintone Server",
			Version: fmt.Sprintf("%s (%s)", Version, Commit),
		},
		Instructions: fmt.Sprintf("kintone is a database service to store and manage enterprise data. You can use this server to interact with kintone."),
	}, nil
}

//go:embed tools_list.json
var toolsListTmplStr string

var toolsList ToolsListResult

func init() {
	tmpl, err := template.New("tools_list").Parse(toolsListTmplStr)
	if err != nil {
		panic(fmt.Sprintf("Failed to parse tools list template: %v", err))
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, nil); err != nil {
		panic(fmt.Sprintf("Failed to render tools list template: %v", err))
	}

	if err := json.Unmarshal(buf.Bytes(), &toolsList); err != nil {
		panic(fmt.Sprintf("Failed to parse tools list JSON: %v", err))
	}
}

func (h *KintoneHandlers) ToolsList(ctx context.Context, params any) (ToolsListResult, error) {
	return toolsList, nil
}

func (h *KintoneHandlers) ToolsCall(ctx context.Context, params ToolsCallRequest) (ToolsCallResult, error) {
	var content *Content
	var err error

	switch params.Name {
	case "listApps":
		content, err = h.ListApps(ctx, params.Arguments)
	case "readAppInfo":
		content, err = h.ReadAppInfo(ctx, params.Arguments)
	case "createRecord":
		content, err = h.CreateRecord(ctx, params.Arguments)
	case "readRecords":
		content, err = h.ReadRecords(ctx, params.Arguments)
	case "updateRecord":
		content, err = h.UpdateRecord(ctx, params.Arguments)
	case "deleteRecord":
		content, err = h.DeleteRecord(ctx, params.Arguments)
	case "readRecordComments":
		content, err = h.ReadRecordComments(ctx, params.Arguments)
	case "createRecordComment":
		content, err = h.CreateRecordComment(ctx, params.Arguments)
	default:
		return ToolsCallResult{}, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: fmt.Sprintf("Unknown tool name: %s", params.Name),
		}
	}

	if err != nil {
		return ToolsCallResult{}, err
	}

	return ToolsCallResult{
		Content: []Content{
			*content,
		},
	}, nil
}

func (h *KintoneHandlers) checkPermissions(id string) error {
	if slices.Contains(h.Deny, id) {
		return jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: fmt.Sprintf("App ID %s is inaccessible because it is listed in the KINTONE_DENY_APPS environment variable. Please check the MCP server settings.", id),
		}
	}
	if len(h.Allow) > 0 && !slices.Contains(h.Allow, id) {
		return jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: fmt.Sprintf("App ID %s is inaccessible because it is not listed in the KINTONE_ALLOW_APPS environment variable. Please check the MCP server settings.", id),
		}
	}

	return nil
}

func (h *KintoneHandlers) ListApps(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		Offset int     `json:"offset"`
		Limit  *int    `json:"limit"`
		Name   *string `json:"name"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.Offset < 0 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Offset must be greater than or equal to 0",
		}
	}
	if req.Limit == nil {
		limit := 100
		req.Limit = &limit
	} else if *req.Limit < 1 || *req.Limit > 100 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Limit must be between 1 and 100",
		}
	}

	var httpRes struct {
		Apps []KintoneAppDetail `json:"apps"`
	}
	err := h.FetchHTTP(ctx, "GET", "/k/v1/apps.json", nil, req, &httpRes)
	if err != nil {
		return nil, err
	}

	apps := make([]KintoneAppDetail, 0, len(httpRes.Apps))
	for _, app := range httpRes.Apps {
		if err := h.checkPermissions(app.AppID); err == nil {
			apps = append(apps, app)
		}
	}

	return JSONContent(JsonMap{
		"apps": apps,
	})
}

func (h *KintoneHandlers) ReadAppInfo(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID string `json:"appID"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.AppID == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Argument 'appID' is required",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	var app KintoneAppDetail
	if err := h.FetchHTTP(ctx, "GET", "/k/v1/app.json", Query{"id": req.AppID}, nil, &app); err != nil {
		return nil, err
	}

	var fields struct {
		Properties JsonMap `json:"properties"`
	}
	if err := h.FetchHTTP(ctx, "GET", "/k/v1/app/form/fields.json", Query{"app": req.AppID}, nil, &fields); err != nil {
		return nil, err
	}

	app.Properties = fields.Properties

	return JSONContent(app)
}

func (h *KintoneHandlers) CreateRecord(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID  string  `json:"appID"`
		Record JsonMap `json:"record"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.AppID == "" || req.Record == nil {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Arguments 'appID' and 'record' are required",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":    req.AppID,
		"record": req.Record,
	}
	var record struct {
		ID string `json:"id"`
	}
	if err := h.FetchHTTP(ctx, "POST", "/k/v1/record.json", nil, httpReq, &record); err != nil {
		return nil, err
	}

	return JSONContent(JsonMap{
		"success":  true,
		"recordID": record.ID,
	})
}

func (h *KintoneHandlers) ReadRecords(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID  string   `json:"appID"`
		Query  string   `json:"query"`
		Limit  *int     `json:"limit"`
		Fields []string `json:"fields"`
		Offset int      `json:"offset"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.AppID == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Argument 'appID' is required",
		}
	}

	if req.Limit == nil {
		limit := 10
		req.Limit = &limit
	} else if *req.Limit < 1 || *req.Limit > 500 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Limit must be between 1 and 500",
		}
	}

	if req.Offset < 0 || req.Offset > 10000 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Offset must be between 0 and 10000",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":        req.AppID,
		"query":      req.Query,
		"limit":      *req.Limit,
		"offset":     req.Offset,
		"fields":     req.Fields,
		"totalCount": true,
	}

	var records JsonMap
	if err := h.FetchHTTP(ctx, "GET", "/k/v1/records.json", nil, httpReq, &records); err != nil {
		return nil, err
	}

	return JSONContent(records)
}

func (h *KintoneHandlers) UpdateRecord(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Record   any    `json:"record"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.AppID == "" || req.RecordID == "" || req.Record == nil {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Arguments 'appID', 'recordID', and 'record' are required",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":    req.AppID,
		"id":     req.RecordID,
		"record": req.Record,
	}
	var result struct {
		Revision string `json:"revision"`
	}
	if err := h.FetchHTTP(ctx, "PUT", "/k/v1/record.json", nil, httpReq, &result); err != nil {
		return nil, err
	}

	return JSONContent(JsonMap{
		"success":  true,
		"revision": result.Revision,
	})
}

func (h *KintoneHandlers) readSingleRecord(ctx context.Context, appID, recordID string) (JsonMap, error) {
	var result struct {
		Record JsonMap `json:"record"`
	}
	err := h.FetchHTTP(ctx, "GET", "/k/v1/record.json", Query{"app": appID, "id": recordID}, nil, &result)

	return result.Record, err
}

func (h *KintoneHandlers) DeleteRecord(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.AppID == "" || req.RecordID == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Arguments 'appID' and 'recordID' are required",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	var deletedRecord JsonMap
	if h.checkPermissions(req.AppID) == nil {
		var err error
		deletedRecord, err = h.readSingleRecord(ctx, req.AppID, req.RecordID)
		if err != nil {
			return nil, err
		}
	}

	if err := h.FetchHTTP(ctx, "DELETE", "/k/v1/records.json", Query{"app": req.AppID, "ids[0]": req.RecordID}, nil, nil); err != nil {
		return nil, err
	}

	result := JsonMap{
		"success": true,
	}
	if deletedRecord != nil {
		result["deletedRecord"] = deletedRecord
	}
	return JSONContent(result)
}

func (h *KintoneHandlers) ReadRecordComments(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Order    string `json:"order"`
		Offset   int    `json:"offset"`
		Limit    *int   `json:"limit"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}

	if req.AppID == "" || req.RecordID == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Arguments 'appID' and 'recordID' are required",
		}
	}

	if req.Order == "" {
		req.Order = "desc"
	} else if req.Order != "asc" && req.Order != "desc" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Order must be 'asc' or 'desc'",
		}
	}

	if req.Offset < 0 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Offset must be greater than or equal to 0",
		}
	}

	if req.Limit == nil {
		limit := 10
		req.Limit = &limit
	} else if *req.Limit < 0 || *req.Limit > 10 {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Limit must be between 1 and 10",
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":    req.AppID,
		"record": req.RecordID,
		"order":  req.Order,
		"offset": req.Offset,
		"limit":  *req.Limit,
	}
	var httpRes struct {
		Comments []JsonMap `json:"comments"`
		Older    bool      `json:"older"`
		Newer    bool      `json:"newer"`
	}
	if err := h.FetchHTTP(ctx, "GET", "/k/v1/record/comments.json", nil, httpReq, &httpRes); err != nil {
		return nil, err
	}

	return JSONContent(JsonMap{
		"comments":            httpRes.Comments,
		"existsOlderComments": httpRes.Older,
		"existsNewerComments": httpRes.Newer,
	})
}

func (h *KintoneHandlers) CreateRecordComment(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Comment  struct {
			Text     string `json:"text"`
			Mentions []struct {
				Code string `json:"code"`
				Type string `json:"type"`
			} `json:"mentions"`
		} `json:"comment"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}

	if req.AppID == "" || req.RecordID == "" || req.Comment.Text == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Arguments 'appID', 'recordID', and 'comment.text' are required",
		}
	}

	for i, m := range req.Comment.Mentions {
		if m.Code == "" {
			return nil, jsonrpc2.Error{
				Code:    jsonrpc2.InvalidParamsCode,
				Message: "Mention code is required",
			}
		}
		if m.Type == "" {
			req.Comment.Mentions[i].Type = "USER"
		} else if m.Type != "USER" && m.Type != "GROUP" && m.Type != "ORGANIZATION" {
			return nil, jsonrpc2.Error{
				Code:    jsonrpc2.InvalidParamsCode,
				Message: "Mention type must be 'USER', 'GROUP', or 'ORGANIZATION'",
			}
		}
	}

	if err := h.checkPermissions(req.AppID); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":     req.AppID,
		"record":  req.RecordID,
		"comment": req.Comment,
	}
	if err := h.FetchHTTP(ctx, "POST", "/k/v1/record/comment.json", nil, httpReq, nil); err != nil {
		return nil, err
	}

	return JSONContent(JsonMap{
		"success": true,
	})
}

func Getenv(key, defaultValue string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultValue
}

func GetenvList(key string) []string {
	if v := os.Getenv(key); v != "" {
		raw := strings.Split(v, ",")
		ss := make([]string, 0, len(raw))
		for _, s := range raw {
			if s != "" {
				ss = append(ss, strings.TrimSpace(s))
			}
		}
		return ss
	}
	return nil
}

type MergedReadWriter struct {
	r io.Reader
	w io.Writer
}

func (rw *MergedReadWriter) Read(p []byte) (int, error) {
	return rw.r.Read(p)
}

func (rw *MergedReadWriter) Write(p []byte) (int, error) {
	return rw.w.Write(p)
}

func main() {
	handlers, err := NewKintoneHandlersFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s\n", err)
		os.Exit(1)
	}

	server := jsonrpc2.NewServer()
	server.On("initialize", jsonrpc2.Call(handlers.InitializeHandler))
	server.On("notifications/initialized", jsonrpc2.Notify(func(ctx context.Context, params any) error {
		return nil
	}))
	server.On("ping", jsonrpc2.Call(func(ctx context.Context, params any) (struct{}, error) {
		return struct{}{}, nil
	}))
	server.On("tools/list", jsonrpc2.Call(handlers.ToolsList))
	server.On("tools/call", jsonrpc2.Call(handlers.ToolsCall))

	fmt.Fprintf(os.Stderr, "kintone server is running on stdio!\n")

	server.ServeForOne(&MergedReadWriter{r: os.Stdin, w: os.Stdout})
}
