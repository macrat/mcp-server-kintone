package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

var (
	Version = "UNKNOWN"
	Commit  = "HEAD"
)

type JsonMap map[string]any

type Request struct {
	JsonRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type ErrorCode int

const (
	ParseError     ErrorCode = -32700
	InvalidRequest ErrorCode = -32600
	MethodNotFound ErrorCode = -32601
	InvalidParams  ErrorCode = -32602
	InternalError  ErrorCode = -32603
)

type ErrorBody struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func NewError(code ErrorCode, message string, data ...any) *ErrorBody {
	return &ErrorBody{
		Code:    code,
		Message: fmt.Sprintf(message, data...),
	}
}

type Response struct {
	JsonRPC string     `json:"jsonrpc"`
	ID      any        `json:"id"`
	Result  any        `json:"result,omitempty"`
	Error   *ErrorBody `json:"error,omitempty"`
}

type RPCConn struct {
	r *json.Decoder
	w *json.Encoder
}

func NewRPCConn(in io.Reader, out io.Writer) RPCConn {
	return RPCConn{
		r: json.NewDecoder(in),
		w: json.NewEncoder(out),
	}
}

func (c RPCConn) Read() (*Request, error) {
	var req Request
	if err := c.r.Decode(&req); err != nil {
		return nil, err
	}
	return &req, nil
}

func (c RPCConn) Write(res Response) error {
	return c.w.Encode(res)
}

type HandlerFunc func(json.RawMessage) (any, *ErrorBody)

type RPCServer struct {
	conn     RPCConn
	handlers map[string]HandlerFunc
}

func NewRPCServer(conn RPCConn) *RPCServer {
	return &RPCServer{
		conn:     conn,
		handlers: make(map[string]HandlerFunc),
	}
}

func (s *RPCServer) SetHandler(method string, handler HandlerFunc) {
	s.handlers[method] = handler
}

func (s *RPCServer) Serve() error {
	for {
		req, err := s.conn.Read()
		if errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return err
		}

		handler, ok := s.handlers[req.Method]
		if !ok {
			if req.ID == nil {
				continue
			}
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Error: &ErrorBody{
					Code:    MethodNotFound,
					Message: "Method not found",
				},
			})
			continue
		}

		res, errBody := handler(req.Params)
		if errBody != nil {
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Error:   errBody,
			})
			continue
		}
		if res != nil {
			s.conn.Write(Response{
				JsonRPC: "2.0",
				ID:      req.ID,
				Result:  res,
			})
		}
	}
}

func IgnoreHandler(params json.RawMessage) (any, *ErrorBody) {
	return nil, nil
}

func PongHandler(params json.RawMessage) (any, *ErrorBody) {
	return JsonMap{}, nil
}

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

func (p *Permissions) UnmarshalJSON(data []byte) error {
	var tmp JsonMap
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}

	if v, ok := tmp["read"]; ok {
		p.Read = v.(bool)
	} else {
		p.Read = true // read is default true
	}
	if v, ok := tmp["write"]; ok {
		p.Write = v.(bool)
	} else {
		p.Write = false
	}
	if v, ok := tmp["delete"]; ok {
		p.Delete = v.(bool)
	} else {
		p.Delete = false
	}

	return nil
}

func UnmarshalJSON[T any](data []byte, target *T) *ErrorBody {
	err := json.Unmarshal(data, target)
	if err != nil {
		return NewError(InvalidParams, "Failed to parse arguments: %v", err)
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

func SendHTTP(h *KintoneHandlers, method, url string, body any, result any) *ErrorBody {
	var reqBody io.Reader
	if body != nil {
		bs, err := json.Marshal(body)
		if err != nil {
			return NewError(InternalError, "Failed to prepare request body for kintone server: %v", err)
		}
		reqBody = bytes.NewReader(bs)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return NewError(InternalError, "Failed to create HTTP request: %v", err)
	}

	req.Header.Set("X-Cybozu-Authorization", h.Auth)
	req.Header.Set("X-Cybozu-API-Token", h.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return NewError(InternalError, "Failed to send HTTP request to kintone server: %v", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(res.Body)
		return NewError(InternalError, "kintone server returned an error: %s: %s", res.Status, msg)
	}

	if result != nil {
		if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
			return NewError(InternalError, "Failed to parse kintone server's response: %s", err)
		}
	}

	return nil
}

func (h *KintoneHandlers) InitializeHandler(params json.RawMessage) (any, *ErrorBody) {
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

func (h *KintoneHandlers) ToolsList(params json.RawMessage) (any, *ErrorBody) {
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

func (h *KintoneHandlers) ToolsCall(params json.RawMessage) (any, *ErrorBody) {
	var req ToolsCallRequest
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	var content any
	var errBody *ErrorBody

	switch req.Name {
	case "listApps":
		content, errBody = h.ListApps(req.Arguments)
	case "readAppInfo":
		content, errBody = h.ReadAppInfo(req.Arguments)
	case "createRecord":
		content, errBody = h.CreateRecord(req.Arguments)
	case "readRecords":
		content, errBody = h.ReadRecords(req.Arguments)
	case "updateRecord":
		content, errBody = h.UpdateRecord(req.Arguments)
	case "deleteRecord":
		content, errBody = h.DeleteRecord(req.Arguments)
	case "readRecordComments":
		content, errBody = h.ReadRecordComments(req.Arguments)
	case "createRecordComment":
		content, errBody = h.CreateRecordComment(req.Arguments)
	default:
		return nil, NewError(InvalidParams, "Unknown tool name: %s", req.Name)
	}

	if errBody != nil {
		return nil, errBody
	}

	bytes, err := json.MarshalIndent(content, "", "  ")
	if err != nil {
		return nil, NewError(InternalError, "Failed to prepare tool response: %v", err)
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

func (h *KintoneHandlers) checkPermissions(id string, ps ...Perm) *ErrorBody {
	app := h.getApp(id)
	if app == nil {
		return NewError(InvalidParams, "App ID %s is not found or not allowed to access", id)
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

	return NewError(InvalidParams, "Permission denied to %s records in app ID %s", strings.Join(ss, ","), id)
}

func (h *KintoneHandlers) ListApps(params json.RawMessage) (any, *ErrorBody) {
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
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/apps.json", h.URL), httpReq, &httpRes)
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

func (h *KintoneHandlers) ReadAppInfo(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID string `json:"appID"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == "" {
		return nil, NewError(InvalidParams, "Argument 'appID' is required")
	}

	if errBody := h.checkPermissions(req.AppID, PermSomething); errBody != nil {
		return nil, errBody
	}

	var app KintoneAppDetail
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/app.json?id=%s", h.URL, req.AppID), nil, &app)
	if errBody != nil {
		return nil, errBody
	}

	app.DescriptionForAI = h.getApp(req.AppID).Description
	app.Permissions = h.getApp(req.AppID).Permissions

	var fields struct {
		Properties JsonMap `json:"properties"`
	}
	errBody = SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/app/form/fields.json?app=%s", h.URL, req.AppID), nil, &fields)

	app.Properties = fields.Properties

	return app, errBody
}

func (h *KintoneHandlers) CreateRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID  string  `json:"appID"`
		Record JsonMap `json:"record"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == "" || req.Record == nil {
		return nil, NewError(InvalidParams, "Arguments 'appID' and 'record' are required")
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
	errBody := SendHTTP(h, "POST", fmt.Sprintf("%s/k/v1/record.json", h.URL), httpReq, &record)

	return JsonMap{
		"success":  true,
		"recordID": record.ID,
	}, errBody
}

func (h *KintoneHandlers) ReadRecords(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID  string   `json:"appID"`
		Query  string   `json:"query"`
		Limit  int      `json:"limit"`
		Fields []string `json:"fields"`
		Offset int      `json:"offset"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == "" {
		return nil, NewError(InvalidParams, "Argument 'appID' is required")
	}

	if req.Limit < 0 || req.Limit > 500 {
		return nil, NewError(InvalidParams, "Limit must be between 1 and 500")
	} else if req.Limit == 0 {
		req.Limit = 10
	}

	if req.Offset < 0 || req.Offset > 10000 {
		return nil, NewError(InvalidParams, "Offset must be between 0 and 10000")
	}

	if err := h.checkPermissions(req.AppID, PermRead); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":        req.AppID,
		"query":      req.Query,
		"limit":      req.Limit,
		"offset":     req.Offset,
		"fields":     req.Fields,
		"totalCount": true,
	}

	var records JsonMap
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/records.json", h.URL), httpReq, &records)
	return records, errBody
}

func (h *KintoneHandlers) UpdateRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Record   any    `json:"record"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == "" || req.RecordID == "" || req.Record == nil {
		return nil, NewError(InvalidParams, "Arguments 'appID', 'recordID', and 'record' are required")
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
	errBody := SendHTTP(h, "PUT", fmt.Sprintf("%s/k/v1/record.json", h.URL), httpReq, &result)
	return JsonMap{
		"success":  true,
		"revision": result.Revision,
	}, errBody
}

func (h *KintoneHandlers) readSingleRecord(appID, recordID string) (JsonMap, *ErrorBody) {
	var result struct {
		Record JsonMap `json:"record"`
	}
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/record.json?app=%s&id=%s", h.URL, appID, recordID), nil, &result)

	return result.Record, errBody
}

func (h *KintoneHandlers) DeleteRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}
	if req.AppID == "" || req.RecordID == "" {
		return nil, NewError(InvalidParams, "Arguments 'appID' and 'recordID' are required")
	}

	if err := h.checkPermissions(req.AppID, PermDelete); err != nil {
		return nil, err
	}

	var deletedRecord JsonMap
	if h.checkPermissions(req.AppID, PermRead) == nil {
		var err *ErrorBody
		deletedRecord, err = h.readSingleRecord(req.AppID, req.RecordID)
		if err != nil {
			return nil, err
		}
	}

	if errBody := SendHTTP(h, "DELETE", fmt.Sprintf("%s/k/v1/records.json?app=%s&ids[0]=%s", h.URL, req.AppID, req.RecordID), nil, nil); errBody != nil {
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

func (h *KintoneHandlers) ReadRecordComments(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID    string `json:"appID"`
		RecordID string `json:"recordID"`
		Order    string `json:"order"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}

	if req.AppID == "" || req.RecordID == "" {
		return nil, NewError(InvalidParams, "Arguments 'appID' and 'recordID' are required")
	}

	if req.Order == "" {
		req.Order = "desc"
	} else if req.Order != "asc" && req.Order != "desc" {
		return nil, NewError(InvalidParams, "Order must be 'asc' or 'desc'")
	}

	if req.Offset < 0 {
		return nil, NewError(InvalidParams, "Offset must be greater than or equal to 0")
	}

	if req.Limit < 0 || req.Limit > 10 {
		return nil, NewError(InvalidParams, "Limit must be between 1 and 10")
	} else if req.Limit == 0 {
		req.Limit = 10
	}

	if err := h.checkPermissions(req.AppID, PermRead); err != nil {
		return nil, err
	}

	httpReq := JsonMap{
		"app":    req.AppID,
		"record": req.RecordID,
		"order":  req.Order,
		"offset": req.Offset,
		"limit":  req.Limit,
	}
	var httpRes struct {
		Comments []JsonMap `json:"comments"`
		Older    bool      `json:"older"`
		Newer    bool      `json:"newer"`
	}
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/record/comments.json", h.URL), httpReq, &httpRes)

	return JsonMap{
		"comments":            httpRes.Comments,
		"existsOlderComments": httpRes.Older,
		"existsNewerComments": httpRes.Newer,
	}, errBody
}

func (h *KintoneHandlers) CreateRecordComment(params json.RawMessage) (any, *ErrorBody) {
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
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}

	if req.AppID == "" || req.RecordID == "" || req.Comment.Text == "" {
		return nil, NewError(InvalidParams, "Arguments 'appID', 'recordID', and 'comment.text' are required")
	}

	for i, m := range req.Comment.Mentions {
		if m.Code == "" {
			return nil, NewError(InvalidParams, "Mention code is required")
		}
		if m.Type == "" {
			req.Comment.Mentions[i].Type = "USER"
		} else if m.Type != "USER" && m.Type != "GROUP" && m.Type != "ORGANIZATION" {
			return nil, NewError(InvalidParams, "Mention type must be 'USER', 'GROUP', or 'ORGANIZATION'")
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
	errBody := SendHTTP(h, "POST", fmt.Sprintf("%s/k/v1/record/comment.json", h.URL), httpReq, nil)

	return JsonMap{
		"success": true,
	}, errBody
}

type KintoneAppConfig struct {
	ID          string      `json:"id"`
	Description string      `json:"description"`
	Permissions Permissions `json:"permissions"`
}

type Configuration struct {
	URL      string             `json:"url"`
	Username string             `json:"username,omitempty"`
	Password string             `json:"password,omitempty"`
	Token    string             `json:"token,omitempty"`
	Apps     []KintoneAppConfig `json:"apps"`
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
		handlers.Auth = base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", conf.Username, conf.Password)))
		handlers.Token = conf.Token
		handlers.Apps = conf.Apps
	}

	server := NewRPCServer(NewRPCConn(os.Stdin, os.Stdout))
	server.SetHandler("initialize", handlers.InitializeHandler)
	server.SetHandler("notifications/initialized", IgnoreHandler)
	server.SetHandler("ping", PongHandler)
	server.SetHandler("tools/list", handlers.ToolsList)
	server.SetHandler("tools/call", handlers.ToolsCall)

	fmt.Fprintf(os.Stderr, "kintone server is running on stdio!\n")

	if err := server.Serve(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
