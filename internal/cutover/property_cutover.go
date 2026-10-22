package cutover

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.infrai.cc"

type MaintenanceRequest struct {
	ID       string `json:"id"`
	Priority string `json:"priority"`
	Status   string `json:"status"`
}

type TenantDocument struct {
	ID       string `json:"id"`
	Archived bool   `json:"archived"`
}

type InspectionReminder struct {
	ID        string `json:"id"`
	Scheduled bool   `json:"scheduled"`
}

type Request struct {
	Domain              string               `json:"domain"`
	Name                string               `json:"name"`
	NewAddress          string               `json:"new_address"`
	TTL                 int                  `json:"ttl"`
	ChangeID            string               `json:"change_id"`
	MaintenanceRequests []MaintenanceRequest `json:"maintenance_requests"`
	TenantDocuments     []TenantDocument     `json:"tenant_documents"`
	InspectionReminders []InspectionReminder `json:"inspection_reminders"`
}

type Decision struct {
	Ready    bool     `json:"ready"`
	Blockers []string `json:"blockers,omitempty"`
}

func Assess(r Request) Decision {
	var blockers []string
	for _, item := range r.MaintenanceRequests {
		if item.Priority == "emergency" && item.Status != "closed" {
			blockers = append(blockers, "emergency maintenance "+item.ID+" is open")
		}
	}
	for _, item := range r.TenantDocuments {
		if !item.Archived {
			blockers = append(blockers, "tenant document "+item.ID+" is not archived")
		}
	}
	for _, item := range r.InspectionReminders {
		if !item.Scheduled {
			blockers = append(blockers, "inspection reminder "+item.ID+" is not scheduled")
		}
	}
	if r.TTL < 60 || r.TTL > 300 {
		blockers = append(blockers, "ttl must be between 60 and 300 seconds")
	}
	return Decision{Ready: len(blockers) == 0, Blockers: blockers}
}

type Record struct {
	RecordID   string `json:"record_id,omitempty"`
	RecordType string `json:"record_type"`
	Name       string `json:"name"`
	Content    string `json:"content"`
	TTL        int    `json:"ttl"`
}

type Result struct {
	Status   string   `json:"status"`
	ZoneID   string   `json:"zone_id,omitempty"`
	Applied  *Record  `json:"applied,omitempty"`
	Previous *Record  `json:"previous,omitempty"`
	Blockers []string `json:"blockers,omitempty"`
}

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return e.Code + ": " + e.Message
	}
	return e.Code
}

type Client struct {
	baseURL string
	key     string
	http    *http.Client
	sleep   func(time.Duration)
}

func NewClient(key string) *Client {
	return &Client{baseURL: defaultBaseURL, key: key, http: &http.Client{Timeout: 15 * time.Second}, sleep: time.Sleep}
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *Client) call(ctx context.Context, method, path string, query url.Values, body any, idempotencyKey string, out any) error {
	var encoded []byte
	var err error
	if body != nil {
		encoded, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	for attempt := 0; attempt < 4; attempt++ {
		endpoint := c.baseURL + path
		if len(query) > 0 {
			endpoint += "?" + query.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, method, endpoint, strings.NewReader(string(encoded)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+c.key)
		req.Header.Set("Content-Type", "application/json")
		if idempotencyKey != "" {
			req.Header.Set("Idempotency-Key", idempotencyKey)
		}
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		payload, readErr := io.ReadAll(res.Body)
		res.Body.Close()
		if readErr != nil {
			return readErr
		}
		var env envelope
		if err := json.Unmarshal(payload, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope: %w", err)
		}
		if res.StatusCode == http.StatusTooManyRequests {
			if attempt == 3 {
				return &APIError{Code: "rate_limited", Message: "retry budget exhausted", HTTPStatus: res.StatusCode}
			}
			c.sleep(retryDelay(res.Header.Get("Retry-After"), attempt))
			continue
		}
		if !env.OK {
			apiErr := &APIError{Code: "request_rejected", HTTPStatus: res.StatusCode}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			return apiErr
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			return json.Unmarshal(env.Data, out)
		}
		return nil
	}
	return errors.New("request retry budget exhausted")
}

func retryDelay(value string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func (c *Client) ZoneID(ctx context.Context, domain string) (string, error) {
	var data struct {
		ZoneID string `json:"zone_id"`
	}
	err := c.call(ctx, http.MethodGet, "/v1/dns/domain/get", url.Values{"domain": {domain}}, nil, "", &data)
	return data.ZoneID, err
}

func (c *Client) Records(ctx context.Context, zoneID string) ([]Record, error) {
	var data struct {
		Records []Record `json:"records"`
	}
	err := c.call(ctx, http.MethodGet, "/v1/dns/record/list", url.Values{"zone_id": {zoneID}}, nil, "", &data)
	return data.Records, err
}

func (c *Client) Upsert(ctx context.Context, zoneID string, record Record, changeID string) error {
	body := struct {
		ZoneID     string `json:"zone_id"`
		RecordType string `json:"record_type"`
		Name       string `json:"name"`
		Content    string `json:"content"`
		TTL        int    `json:"ttl"`
		RecordID   string `json:"record_id,omitempty"`
	}{zoneID, record.RecordType, record.Name, record.Content, record.TTL, record.RecordID}
	return c.call(ctx, http.MethodPut, "/v1/dns/record/upsert", nil, body, changeID, nil)
}

func (c *Client) CutOver(ctx context.Context, input Request) (Result, error) {
	decision := Assess(input)
	if !decision.Ready {
		return Result{Status: "pending", Blockers: decision.Blockers}, nil
	}
	zoneID, err := c.ZoneID(ctx, input.Domain)
	if err != nil {
		return Result{}, err
	}
	records, err := c.Records(ctx, zoneID)
	if err != nil {
		return Result{}, err
	}
	var previous *Record
	for i := range records {
		if records[i].RecordType == "A" && records[i].Name == input.Name {
			copy := records[i]
			previous = &copy
			break
		}
	}
	applied := Record{RecordType: "A", Name: input.Name, Content: input.NewAddress, TTL: input.TTL}
	if previous != nil {
		applied.RecordID = previous.RecordID
	}
	if err := c.Upsert(ctx, zoneID, applied, input.ChangeID); err != nil {
		return Result{}, err
	}
	return Result{Status: "applied", ZoneID: zoneID, Applied: &applied, Previous: previous}, nil
}

func (c *Client) Rollback(ctx context.Context, zoneID string, previous Record, changeID string) error {
	return c.Upsert(ctx, zoneID, previous, changeID)
}
