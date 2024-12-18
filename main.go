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

type ResourceInfo struct {
	URI         string `json:"uri"`
	Name        string `json:"name"`
	Description string `json:"description"`
	MimeType    string `json:"mimeType"`
}

type ResourcesListResult struct {
	Resources []ResourceInfo `json:"resources"`
}

type ResourcesReadRequest struct {
	URI string `json:"uri"`
}

type ResourcesReadResult struct {
	URI     string    `json:"uri"`
	Content []Content `json:"content"`
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

type KintoneAppInfo struct {
	ID          int    `json:"appID"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type KintoneHandlers struct {
	URL   string
	Auth  string
	Token string
	Apps  []KintoneAppInfo
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
							"type":        "number",
							"description": "The record ID to update.",
						},
						"record": kintoneRecord,
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

	switch req.Name {
	case "listApps":
		return h.ListApps(req.Arguments)
	case "readAppInfo":
		return h.ReadAppInfo(req.Arguments)
	case "readRecords":
		return h.ReadRecords(req.Arguments)
	case "createRecord":
		return h.CreateRecord(req.Arguments)
	case "updateRecord":
		return h.UpdateRecord(req.Arguments)
	default:
		return nil, NewError(InvalidParams, "Unknown tool name: %s", req.Name)
	}
}

func (h *KintoneHandlers) isAppExported(id int) bool {
	for _, app := range h.Apps {
		if app.ID == id {
			return true
		}
	}
	return false
}

func (h *KintoneHandlers) ListApps(params json.RawMessage) (any, *ErrorBody) {
	resp, err := json.MarshalIndent(h.Apps, "", "  ")
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal response: %v", err)
	}
	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: string(resp)},
		},
	}, nil
}

func (h *KintoneHandlers) ReadAppInfo(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID int `json:"appID"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	if !h.isAppExported(req.AppID) {
		return nil, NewError(InvalidParams, "App ID %d is not found or not exported", req.AppID)
	}

	hreq, err := http.NewRequest("GET", fmt.Sprintf("%s/k/v1/app/form/fields.json?app=%d", h.URL, req.AppID), nil)
	if err != nil {
		return nil, NewError(InternalError, "Failed to create HTTP request: %v", err)
	}
	hreq.Header.Set("X-Cybozu-Authorization", h.Auth)
	hreq.Header.Set("X-Cybozu-API-Token", h.Token)

	hres, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return nil, NewError(InternalError, "Failed to send HTTP request: %v", err)
	}
	defer hres.Body.Close()

	if hres.StatusCode != http.StatusOK {
		mesg, _ := io.ReadAll(hres.Body)
		return nil, NewError(InternalError, "HTTP request failed: %s: %s", hres.Status, mesg)
	}

	var fields struct {
		Properties map[string]JsonMap `json:"properties"`
	}
	if err := json.NewDecoder(hres.Body).Decode(&fields); err != nil {
		return nil, NewError(InternalError, "Failed to parse response: %v", err)
	}

	result := JsonMap{
		"appID":      req.AppID,
		"properties": fields.Properties,
	}
	resp, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal response: %v", err)
	}
	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: string(resp)},
		},
	}, nil
}

func (h *KintoneHandlers) ReadRecords(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID  int    `json:"appID"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
		Offset int    `json:"offset"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	if req.Limit < 0 || req.Limit > 500 {
		return nil, NewError(InvalidParams, "Limit must be between 1 and 500")
	} else if req.Limit == 0 {
		req.Limit = 10
	}

	if req.Offset < 0 || req.Offset > 10000 {
		return nil, NewError(InvalidParams, "Offset must be between 0 and 10000")
	}

	if !h.isAppExported(req.AppID) {
		return nil, NewError(InvalidParams, "App ID %d is not found or not exported", req.AppID)
	}

	query := url.Values{}
	query.Set("app", fmt.Sprintf("%d", req.AppID))
	query.Set("query", req.Query)
	query.Set("limit", fmt.Sprintf("%d", req.Limit))
	query.Set("offset", fmt.Sprintf("%d", req.Offset))
	query.Set("totalCount", "true")

	hreq, err := http.NewRequest("GET", fmt.Sprintf("%s/k/v1/records.json?%s", h.URL, query.Encode()), nil)
	if err != nil {
		return nil, NewError(InternalError, "Failed to create HTTP request: %v", err)
	}
	hreq.Header.Set("X-Cybozu-Authorization", h.Auth)
	hreq.Header.Set("X-Cybozu-API-Token", h.Token)

	hres, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return nil, NewError(InternalError, "Failed to send HTTP request: %v", err)
	}
	defer hres.Body.Close()

	if hres.StatusCode != http.StatusOK {
		mesg, _ := io.ReadAll(hres.Body)
		return nil, NewError(InternalError, "HTTP request failed: %s: %s", hres.Status, mesg)
	}

	var result JsonMap
	if err := json.NewDecoder(hres.Body).Decode(&result); err != nil {
		return nil, NewError(InternalError, "Failed to parse response: %v", err)
	}

	resp, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal response: %v", err)
	}
	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: string(resp)},
		},
	}, nil
}

func (h *KintoneHandlers) CreateRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID  int `json:"appID"`
		Record any `json:"record"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	if !h.isAppExported(req.AppID) {
		return nil, NewError(InvalidParams, "App ID %d is not found or not exported", req.AppID)
	}

	body := JsonMap{
		"app":    req.AppID,
		"record": req.Record,
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal request body: %v", err)
	}

	hreq, err := http.NewRequest("POST", fmt.Sprintf("%s/k/v1/record.json", h.URL), bytes.NewReader(reqBody))
	if err != nil {
		return nil, NewError(InternalError, "Failed to create HTTP request: %v", err)
	}
	hreq.Header.Set("X-Cybozu-Authorization", h.Auth)
	hreq.Header.Set("X-Cybozu-API-Token", h.Token)
	hreq.Header.Set("Content-Type", "application/json")

	hres, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return nil, NewError(InternalError, "Failed to send HTTP request: %v", err)
	}
	defer hres.Body.Close()

	if hres.StatusCode != http.StatusOK {
		mesg, _ := io.ReadAll(hres.Body)
		return nil, NewError(InternalError, "HTTP request failed: %s: %s", hres.Status, mesg)
	}

	var resBody struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(hres.Body).Decode(&resBody); err != nil {
		return nil, NewError(InternalError, "Failed to parse response: %v", err)
	}

	result := JsonMap{
		"success":  true,
		"recordID": resBody.ID,
	}
	resp, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal response: %v", err)
	}
	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: string(resp)},
		},
	}, nil
}

func (h *KintoneHandlers) UpdateRecord(params json.RawMessage) (any, *ErrorBody) {
	var req struct {
		AppID    int `json:"appID"`
		RecordID int `json:"recordID"`
		Record   any `json:"record"`
	}

	if err := json.Unmarshal(params, &req); err != nil {
		return nil, NewError(InvalidParams, "Failed to parse parameters: %v", err)
	}

	if !h.isAppExported(req.AppID) {
		return nil, NewError(InvalidParams, "App ID %d is not found or not exported", req.AppID)
	}

	body := JsonMap{
		"app":    req.AppID,
		"id":     req.RecordID,
		"record": req.Record,
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		return nil, NewError(InternalError, "Failed to marshal request body: %v", err)
	}

	hreq, err := http.NewRequest("PUT", fmt.Sprintf("%s/k/v1/record.json", h.URL), bytes.NewReader(reqBody))
	if err != nil {
		return nil, NewError(InternalError, "Failed to create HTTP request: %v", err)
	}
	hreq.Header.Set("X-Cybozu-Authorization", h.Auth)
	hreq.Header.Set("X-Cybozu-API-Token", h.Token)
	hreq.Header.Set("Content-Type", "application/json")

	hres, err := http.DefaultClient.Do(hreq)
	if err != nil {
		return nil, NewError(InternalError, "Failed to send HTTP request: %v", err)
	}
	defer hres.Body.Close()

	if hres.StatusCode != http.StatusOK {
		mesg, _ := io.ReadAll(hres.Body)
		return nil, NewError(InternalError, "HTTP request failed: %s: %s", hres.Status, mesg)
	}

	return ToolsCallResult{
		Content: []Content{
			{Type: "text", Text: `{"success": true}`},
		},
	}, nil
}

type KintoneAppConfig struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
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
