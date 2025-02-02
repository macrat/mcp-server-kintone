package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

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
	URL   string
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

func (h *KintoneHandlers) SendHTTP(method, url string, query Query, body any, result any) error {
	var reqBody io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			return jsonrpc2.Error{
				Code:    jsonrpc2.InternalErrorCode,
				Message: fmt.Sprintf("Failed to prepare request body for kintone server: %v", err),
			}
		}
		reqBody = bytes.NewReader(bs)
	}

	if query != nil {
		url += "?" + query.Encode()
	}

	req, err := http.NewRequest(method, h.URL+url, reqBody)
	if err != nil {
		return jsonrpc2.Error{
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
		return jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: fmt.Sprintf("Failed to send HTTP request to kintone server: %v", err),
		}
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: "kintone server returned an error",
			Data:    JsonMap{"statusCode": res.Status, "message": string(msg)},
		}
	}

	if result != nil {
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
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

func (h *KintoneHandlers) ToolsList(ctx context.Context, params any) (ToolsListResult, error) {
	kintoneRecord := JsonMap{
		"type":        "object",
		"description": `The record data to create. Record data format is the same as kintone's record data format. For example, {"field1": {"value": "value1"}, "field2": {"value": "value2"}, "field3": {"value": "value3"}}.`,
		"additionalProperties": JsonMap{
			"type":     "object",
			"required": []string{"value"},
			"properties": JsonMap{
				"value": JsonMap{
					"anyOf": []JsonMap{
						{
							"type":        "string",
							"description": "Usual values for text, number, etc.",
						},
						{
							"type":        "array",
							"description": "Values for checkbox.",
							"items": JsonMap{
								"type": "string",
							},
						},
						{
							"type":        "object",
							"description": "Values for table.",
							"required":    []string{"value"},
							"properties": JsonMap{
								"value": JsonMap{
									"type": "array",
									"items": JsonMap{
										"type":     "object",
										"required": []string{"value"},
										"properties": JsonMap{
											"value": JsonMap{
												"type": "object",
												"additionalProperties": JsonMap{
													"type":     "object",
													"required": []string{"value"},
													"properties": JsonMap{
														"value": JsonMap{},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return ToolsListResult{
		Tools: []ToolInfo{
			{
				Name:        "listApps",
				Description: "List all applications made on kintone. Response includes the app ID, name, and description.",
				InputSchema: JsonMap{
					"type":       "object",
					"properties": JsonMap{},
				},
			},
			{
				Name:        "readAppInfo",
				Description: "Get information about the specified app. Response includes the app ID, name, description, and schema.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to get information from.",
						},
					},
				},
			},
			{
				Name:        "createRecord",
				Description: "Create a new record in the specified app. Before use this tool, you better to know the schema of the app by using 'readAppInfo' tool.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID", "record"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to create a record in.",
						},
						"record": kintoneRecord,
					},
				},
			},
			{
				Name:        "readRecords",
				Description: "Read records from the specified app. Response includes the record ID and record data. Before search records using this tool, you better to know the schema of the app by using 'readAppInfo' tool.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to read records from.",
						},
						"query": JsonMap{
							"type":        "string",
							"description": "The query to filter records. Query format is the same as kintone's query format. For example, 'field1 = \"value1\" and (field2 like \"value2\"' or field3 not in (\"value3.1\",\"value3.2\")) and date > \"2006-01-02\"'.",
						},
						"fields": JsonMap{
							"type":        "array",
							"description": "The field codes to include in the response. Default is all fields.",
							"items": JsonMap{
								"type": "string",
							},
						},
						"limit": JsonMap{
							"type":        "number",
							"description": "The maximum number of records to read. Default is 10, maximum is 500.",
						},
						"offset": JsonMap{
							"type":        "number",
							"description": "The offset of records to read. Default is 0, maximum is 10,000.",
						},
					},
				},
			},
			{
				Name:        "updateRecord",
				Description: "Update the specified record in the specified app. Before use this tool, you better to know the schema of the app by using 'readAppInfo' tool and check which record to update by using 'readRecords' tool.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID", "recordID", "record"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to update a record in.",
						},
						"recordID": JsonMap{
							"type":        "string",
							"description": "The record ID to update.",
						},
						"record": kintoneRecord,
					},
				},
			},
			{
				Name:        "deleteRecord",
				Description: "Delete the specified record in the specified app. Before use this tool, you should check which record to delete by using 'readRecords' tool. This operation is unrecoverable, so make sure that the user really want to delete the record.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID", "recordID"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to delete a record from.",
						},
						"recordID": JsonMap{
							"type":        "string",
							"description": "The record ID to delete.",
						},
					},
				},
			},
			{
				Name:        "readRecordComments",
				Description: "Read comments on the specified record in the specified app.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID", "recordID"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to read comments from.",
						},
						"recordID": JsonMap{
							"type":        "string",
							"description": "The record ID to read comments from.",
						},
						"order": JsonMap{
							"type":        "string",
							"description": "The order of comments. Default is 'desc'.",
						},
						"offset": JsonMap{
							"type":        "number",
							"description": "The offset of comments to read. Default is 0.",
						},
						"limit": JsonMap{
							"type":        "number",
							"description": "The maximum number of comments to read. Default is 10, maximum is 10.",
						},
					},
				},
			},
			{
				Name:        "createRecordComment",
				Description: "Create a new comment on the specified record in the specified app.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID", "recordID", "comment"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "string",
							"description": "The app ID to create a comment in.",
						},
						"recordID": JsonMap{
							"type":        "string",
							"description": "The record ID to create a comment on.",
						},
						"comment": JsonMap{
							"type":     "object",
							"required": []string{"text"},
							"properties": JsonMap{
								"text": JsonMap{
									"type":        "string",
									"description": "The text of the comment.",
								},
								"mentions": JsonMap{
									"type":        "array",
									"description": "The mention targets of the comment. The target can be a user, a group, or a organization.",
									"items": JsonMap{
										"type":     "object",
										"required": []string{"code"},
										"properties": JsonMap{
											"code": JsonMap{
												"type":        "string",
												"description": "The code of the mention target. You can get the code by other records or comments.",
											},
											"type": JsonMap{
												"type":        "string",
												"description": "The type of the mention target. Default is 'USER'.",
												"enum":        []string{"USER", "GROUP", "ORGANIZATION"},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}, nil
}

func (h *KintoneHandlers) ToolsCall(ctx context.Context, params ToolsCallRequest) (ToolsCallResult, error) {
	var content any
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

	bytes, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return ToolsCallResult{}, jsonrpc2.Error{
			Code:    jsonrpc2.InternalErrorCode,
			Message: fmt.Sprintf("Failed to prepare tool response: %v", err),
		}
	}

	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: string(bytes)},
		},
	}, nil
}

func (h *KintoneHandlers) getApp(id string) *KintoneAppConfig {
	for _, app := range h.Apps {
		if app.ID == id {
			return &app
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

func (h *KintoneHandlers) ListApps(ctx context.Context, params json.RawMessage) (any, error) {
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
	errBody := h.SendHTTP("GET", "/k/v1/apps.json", nil, httpReq, &httpRes)
	if errBody != nil {
		return nil, errBody
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

	return httpRes.Apps, nil
}

func (h *KintoneHandlers) ReadAppInfo(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		AppID string `json:"appID"`
	}
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == "" {
		return nil, jsonrpc2.Error{
			Code:    jsonrpc2.InvalidParamsCode,
			Message: "Argument 'appID' is required",
		}
	}

	if errBody := h.checkPermissions(req.AppID, PermSomething); errBody != nil {
		return nil, errBody
	}

	var app KintoneAppDetail
	errBody := h.SendHTTP("GET", "/k/v1/app.json", Query{"id": req.AppID}, nil, &app)
	if errBody != nil {
		return nil, errBody
	}

	app.DescriptionForAI = h.getApp(req.AppID).Description
	app.Permissions = h.getApp(req.AppID).Permissions

	var fields struct {
		Properties JsonMap `json:"properties"`
	}
	errBody = h.SendHTTP("GET", "/k/v1/app/form/fields.json", Query{"app": req.AppID}, nil, &fields)

	app.Properties = fields.Properties

	return app, errBody
}

func (h *KintoneHandlers) CreateRecord(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		AppID  string  `json:"appID"`
		Record JsonMap `json:"record"`
	}
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
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
	errBody := h.SendHTTP("POST", "/k/v1/record.json", nil, httpReq, &record)

	return JsonMap{
		"success":  true,
		"recordID": record.ID,
	}, errBody
}

func (h *KintoneHandlers) ReadRecords(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		AppID  string   `json:"appID"`
		Query  string   `json:"query"`
		Limit  *int     `json:"limit"`
		Fields []string `json:"fields"`
		Offset int      `json:"offset"`
	}
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
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
	errBody := h.SendHTTP("GET", "/k/v1/records.json", nil, httpReq, &records)
	return records, errBody
}

func (h *KintoneHandlers) UpdateRecord(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Record   any    `json:"record"`
	}
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
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
	errBody := h.SendHTTP("PUT", "/k/v1/record.json", nil, httpReq, &result)
	return JsonMap{
		"success":  true,
		"revision": result.Revision,
	}, errBody
}

func (h *KintoneHandlers) readSingleRecord(ctx context.Context, appID, recordID string) (JsonMap, error) {
	var result struct {
		Record JsonMap `json:"record"`
	}
	errBody := h.SendHTTP("GET", "/k/v1/record.json", Query{"app": appID, "id": recordID}, nil, &result)

	return result.Record, errBody
}

func (h *KintoneHandlers) DeleteRecord(ctx context.Context, params json.RawMessage) (any, error) {
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

	if errBody := h.SendHTTP("DELETE", "/k/v1/records.json", Query{"app": req.AppID, "ids[0]": req.RecordID}, nil, nil); errBody != nil {
		return nil, errBody
	}

	result := JsonMap{
		"success": true,
	}
	if deletedRecord != nil {
		result["deletedRecord"] = deletedRecord
	}
	return result, nil
}

func (h *KintoneHandlers) ReadRecordComments(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Order    string `json:"order"`
		Offset   int    `json:"offset"`
		Limit    *int   `json:"limit"`
	}
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
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
	errBody := h.SendHTTP("GET", "/k/v1/record/comments.json", nil, httpReq, &httpRes)

	return JsonMap{
		"comments":            httpRes.Comments,
		"existsOlderComments": httpRes.Older,
		"existsNewerComments": httpRes.Newer,
	}, errBody
}

func (h *KintoneHandlers) CreateRecordComment(ctx context.Context, params json.RawMessage) (any, error) {
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
	if errBody := UnmarshalParams(params, &req); errBody != nil {
		return nil, errBody
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
	errBody := h.SendHTTP("POST", "/k/v1/record/comment.json", nil, httpReq, nil)

	return JsonMap{
		"success": true,
	}, errBody
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

		handlers.URL = conf.URL
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
