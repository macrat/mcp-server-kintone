package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
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

type KintoneAppInfo struct {
	ID          int         `json:"appID"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Permissions Permissions `json:"permissions"`
}

type KintoneHandlers struct {
	URL   string
	Auth  string
	Token string
	Apps  []KintoneAppInfo
}

func SendHTTP(h *KintoneHandlers, method, url string, body any, result any) *ErrorBody {
	reqBody, err := json.Marshal(body)
	if err != nil {
		return NewError(InternalError, "Failed to prepare request body for kintone server: %v", err)
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(reqBody))
	if err != nil {
		return NewError(InternalError, "Failed to create HTTP request: %v", err)
	}

	req.Header.Set("X-Cybozu-Authorization", h.Auth)
	req.Header.Set("X-Cybozu-API-Token", h.Token)
	req.Header.Set("Content-Type", "application/json")

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
							"type":        "number",
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
							"type":        "number",
							"description": "The app ID to create a record in.",
						},
						"record": kintoneRecord,
					},
				},
			},
			{
				Name:        "readRecords",
				Description: "Read records from the specified app. Response includes the record ID, record number, and record data. Before use this tool, you better to know the schema of the app by using 'readAppInfo' tool.",
				InputSchema: JsonMap{
					"type":     "object",
					"required": []string{"appID"},
					"properties": JsonMap{
						"appID": JsonMap{
							"type":        "number",
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
							"type":        "number",
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
							"type":        "number",
							"description": "The app ID to delete a record from.",
						},
						"recordID": JsonMap{
							"type":        "string",
							"description": "The record ID to delete.",
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

func (h *KintoneHandlers) getApp(id int) *KintoneAppInfo {
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

func (h *KintoneHandlers) checkPermissions(id int, p Perm) *ErrorBody {
	app := h.getApp(id)
	if app == nil {
		return NewError(InvalidParams, "App ID %d is not found or not allowed to access", id)
	}
	if (p == PermRead && app.Permissions.Read) || (p == PermWrite && app.Permissions.Write) || (p == PermDelete && app.Permissions.Delete) || (p == PermSomething) {
		return nil
	}
	return NewError(InvalidParams, "Permission denied to %s records in app ID %d", p, id)
}

func (h *KintoneHandlers) ListApps(params json.RawMessage) (any, *ErrorBody) {
	return h.Apps, nil
}

func (h *KintoneHandlers) ReadAppInfo(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID int `json:"appID"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == 0 {
		return nil, NewError(InvalidParams, "Argument 'appID' is required")
	}

	if errBody := h.checkPermissions(req.AppID, PermSomething); errBody != nil {
		return nil, errBody
	}

	var fields struct {
		Properties map[string]JsonMap `json:"properties"`
	}
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/app/form/fields.json?app=%d", h.URL, req.AppID), nil, &fields)

	return JsonMap{
		"appID":      req.AppID,
		"properties": fields.Properties,
	}, errBody
}

func (h *KintoneHandlers) CreateRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID  int `json:"appID"`
		Record any `json:"record"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == 0 || req.Record == nil {
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
		AppID  int      `json:"appID"`
		Query  string   `json:"query"`
		Limit  int      `json:"limit"`
		Fields []string `json:"fields"`
		Offset int      `json:"offset"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == 0 {
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
		AppID    int    `json:"appID"`
		RecordID string `json:"recordID"`
		Record   any    `json:"record"`
	}
	if errBody := UnmarshalJSON(params, &req); errBody != nil {
		return nil, errBody
	}
	if req.AppID == 0 || req.RecordID == "" || req.Record == nil {
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

func (h *KintoneHandlers) readSingleRecord(appID int, recordID string) (JsonMap, *ErrorBody) {
	var result struct {
		Record JsonMap `json:"record"`
	}
	errBody := SendHTTP(h, "GET", fmt.Sprintf("%s/k/v1/record.json?app=%d&id=%s", h.URL, appID, recordID), nil, &result)

	return result.Record, errBody
}

func (h *KintoneHandlers) DeleteRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID    int    `json:"appID"`
		RecordID string `json:"recordID"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}
	if req.AppID == 0 || req.RecordID == "" {
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

	if errBody := SendHTTP(h, "DELETE", fmt.Sprintf("%s/k/v1/records.json?app=%d&ids[0]=%s", h.URL, req.AppID, req.RecordID), nil, nil); errBody != nil {
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

type KintoneAppConfig struct {
	ID          int         `json:"id"`
	Name        string      `json:"name"`
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
		for _, app := range conf.Apps {
			handlers.Apps = append(handlers.Apps, KintoneAppInfo{
				ID:          app.ID,
				Name:        app.Name,
				Description: app.Description,
				Permissions: app.Permissions,
			})
		}
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
