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

type Resource struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType"`
	Text     string `json:"text,omitempty"`
	Blob     string `json:"blob,omitempty"`
}

func NewResource(uri, mimeType string, r io.Reader) (*Resource, error) {
	if strings.HasPrefix(mimeType, "text/") {
		bs, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		return &Resource{URI: uri, MIMEType: mimeType, Text: string(bs)}, nil
	} else {
		var buf bytes.Buffer
		enc := base64.NewEncoder(base64.StdEncoding, &buf)
		if _, err := io.Copy(enc, r); err != nil {
			return nil, err
		}
		return &Resource{URI: uri, MIMEType: mimeType, Blob: buf.String()}, nil
	}
}

type Content struct {
	Type     string   `json:"type"`
	Text     string   `json:"text,omitempty"`
	Resource Resource `json:"resource,omitempty"`
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

type Permissions struct {
	Read   bool `json:"read"`
	Write  bool `json:"write"`
	Delete bool `json:"delete"`
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
	AppID            string      `json:"appID"`
	Name             string      `json:"name"`
	Description      string      `json:"description,omitempty"`
	DescriptionForAI string      `json:"description_for_ai,omitempty"`
	Properties       JsonMap     `json:"properties,omitempty"`
	CreatedAt        string      `json:"createdAt"`
	ModifiedAt       string      `json:"modifiedAt"`
	Permissions      Permissions `json:"permissions"`
}

type KintoneHandlers struct {
	URL   *url.URL
	Auth  string
	Token string
	Apps  []KintoneAppConfig
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
	case "downloadAttachmentFile":
		content, err = h.DownloadAttachmentFile(ctx, params.Arguments)
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

func (h *KintoneHandlers) getApp(id string) *KintoneAppConfig {
	for i, app := range h.Apps {
		if app.ID == id {
			return &h.Apps[i]
		}
	}
	return nil
}

type Perm string

const (
	PermSomething Perm = "do something"
	PermRead      Perm = "read"
	PermWrite     Perm = "write"
	PermDelete    Perm = "delete"
)

func (h *KintoneHandlers) checkPermissions(id string, ps ...Perm) error {
	app := h.getApp(id)
	if app == nil {
		return jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: fmt.Sprintf("App ID %s is not found or not allowed to access. Please check the MCP server settings and/or ask to the administrator.", id),
		}
	}
	for _, p := range ps {
		if (p == PermRead && app.Permissions.Read) || (p == PermWrite && app.Permissions.Write) || (p == PermDelete && app.Permissions.Delete) || (p == PermSomething) {
			return nil
		}
	}

	ss := make([]string, len(ps))
	for i, p := range ps {
		ss[i] = string(p)
	}

	return jsonrpc2.Error{
		Code:    jsonrpc2.InvalidParamsCode,
		Message: fmt.Sprintf("Permission denied to %s records in app ID %s. Please check the MCP server settings and/or ask to the administrator.", strings.Join(ss, ", "), id),
	}
}

func (h *KintoneHandlers) ListApps(ctx context.Context, params json.RawMessage) (*Content, error) {
	type HTTPReq struct {
		IDs []string `json:"ids"`
	}
	var httpReq HTTPReq
	for _, app := range h.Apps {
		httpReq.IDs = append(httpReq.IDs, app.ID)
	}

	var httpRes struct {
		Apps []KintoneAppDetail `json:"apps"`
	}
	err := h.FetchHTTP(ctx, "GET", "/k/v1/apps.json", nil, httpReq, &httpRes)
	if err != nil {
		return nil, err
	}

	for i, a := range httpRes.Apps {
		for _, b := range h.Apps {
			if a.AppID == b.ID {
				httpRes.Apps[i].DescriptionForAI = b.Description
				httpRes.Apps[i].Permissions = b.Permissions
				break
			}
		}
	}

	return JSONContent(httpRes.Apps)
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

	if err := h.checkPermissions(req.AppID, PermSomething); err != nil {
		return nil, err
	}

	var app KintoneAppDetail
	if err := h.FetchHTTP(ctx, "GET", "/k/v1/app.json", Query{"id": req.AppID}, nil, &app); err != nil {
		return nil, err
	}

	app.DescriptionForAI = h.getApp(req.AppID).Description
	app.Permissions = h.getApp(req.AppID).Permissions

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

	if err := h.checkPermissions(req.AppID, PermWrite); err != nil {
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

	if err := h.checkPermissions(req.AppID, PermRead); err != nil {
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

	if err := h.checkPermissions(req.AppID, PermWrite); err != nil {
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

	if err := h.checkPermissions(req.AppID, PermDelete); err != nil {
		return nil, err
	}

	var deletedRecord JsonMap
	if h.checkPermissions(req.AppID, PermRead) == nil {
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

func (h *KintoneHandlers) DownloadAttachmentFile(ctx context.Context, params json.RawMessage) (*Content, error) {
	var req struct {
		FileKey string `json:"fileKey"`
	}
	if err := UnmarshalParams(params, &req); err != nil {
		return nil, err
	}
	if req.FileKey == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Argument 'fileKey' is required",
		}
	}

	httpRes, err := h.SendHTTP(ctx, "GET", "/k/v1/file.json", Query{"fileKey": req.FileKey}, nil)
	if err != nil {
		return nil, err
	}
	defer httpRes.Body.Close()

	contentType := httpRes.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	resource, err := NewResource("kintone://file/"+req.FileKey, contentType, httpRes.Body)
	if err != nil {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: fmt.Sprintf("Failed to read attachment file: %v", err),
		}
	}

	return &Content{
		Type:     "resource",
		Resource: *resource,
	}, nil
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

	if err := h.checkPermissions(req.AppID, PermRead); err != nil {
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

	if err := h.checkPermissions(req.AppID, PermRead, PermWrite); err != nil {
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

type KintoneAppConfig struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Permissions Permissions `json:"permissions"`
}

func (a *KintoneAppConfig) UnmarshalJSON(data []byte) error {
	var tmp struct {
		ID          string  `json:"id"`
		Description string  `json:"description"`
		Permissions JsonMap `json:"permissions"`
	}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	a.ID = tmp.ID
	a.Description = tmp.Description

	getPerm := func(key string, default_ bool) (bool, error) {
		if v, ok := tmp.Permissions[key]; ok {
			if b, ok := v.(bool); ok {
				return b, nil
			} else {
				return false, errors.New("members of 'permissions' must be boolean")
			}
		} else {
			return default_, nil
		}
	}

	var err error
	if a.Permissions.Read, err = getPerm("read", true); err != nil {
		return err
	}
	if a.Permissions.Write, err = getPerm("write", false); err != nil {
		return err
	}
	if a.Permissions.Delete, err = getPerm("delete", false); err != nil {
		return err
	}

	return nil
}

type Configuration struct {
	URL      string             `json:"url"`
	Username string             `json:"username,omitempty"`
	Password string             `json:"password,omitempty"`
	Token    string             `json:"token,omitempty"`
	Apps     []KintoneAppConfig `json:"apps"`
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
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "MCP (Model Context Protocol) Server for kintone.\n")
		fmt.Fprintf(os.Stderr, "Version: %s (%s)\n", Version, Commit)
		fmt.Fprintf(os.Stderr, "Usage: %s <path to settings file>\n", os.Args[0])
		os.Exit(1)
	}

	handlers := &KintoneHandlers{}

	if f, err := os.Open(os.Args[1]); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read config file: %v\n", err)
		os.Exit(1)
	} else {
		var conf Configuration
		if err := json.NewDecoder(f).Decode(&conf); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse config file: %v\n", err)
			os.Exit(1)
		}

		if (conf.Username == "" || conf.Password == "") && conf.Token == "" {
			fmt.Fprintf(os.Stderr, "Either username/password or token must be provided\n")
			os.Exit(1)
		}
		if len(conf.Apps) == 0 {
			fmt.Fprintf(os.Stderr, "At least one app must be provided\n")
			os.Exit(1)
		}

		if u, err := url.Parse(conf.URL); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to parse URL: %v\n", err)
			os.Exit(1)
		} else {
			handlers.URL = u
		}
		if conf.Username != "" && conf.Password != "" {
			handlers.Auth = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", conf.Username, conf.Password)))
		}
		handlers.Token = conf.Token
		handlers.Apps = conf.Apps
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
