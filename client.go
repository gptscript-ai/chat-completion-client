package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"slices"
	"strings"
	"time"
)

// Client is OpenAI GPT-3 API client.
type Client struct {
	config ClientConfig
}

type Response interface {
	SetHeader(http.Header)
}

type httpHeader http.Header

func (h *httpHeader) SetHeader(header http.Header) {
	*h = httpHeader(header)
}

func (h *httpHeader) Header() http.Header {
	return http.Header(*h)
}

func (h *httpHeader) GetRateLimitHeaders() RateLimitHeaders {
	return newRateLimitHeaders(h.Header())
}

// NewClient creates new OpenAI API client.
func NewClient(authToken string) *Client {
	config := DefaultConfig(authToken)
	return NewClientWithConfig(config)
}

// NewClientWithConfig creates new OpenAI API client for specified config.
func NewClientWithConfig(config ClientConfig) *Client {
	return &Client{
		config: config,
	}
}

func (c *Client) GetAPIKeyAndBaseURL() (string, string) {
	return c.config.authToken, c.config.BaseURL
}

func (c *Client) SetAPIKey(apiKey string) {
	c.config.authToken = apiKey
}

type requestOptions struct {
	body   any
	header http.Header
}

type requestOption func(*requestOptions)

func withBody(body any) requestOption {
	return func(args *requestOptions) {
		args.body = body
	}
}

func (c *Client) newRequest(ctx context.Context, method, url string, setters ...requestOption) (*http.Request, error) {
	// Default Options
	args := &requestOptions{
		body:   nil,
		header: make(http.Header),
	}
	for _, setter := range setters {
		setter(args)
	}
	req, err := newHTTPRequest(ctx, method, url, args.body, args.header)
	if err != nil {
		return nil, err
	}
	c.setCommonHeaders(req)
	return req, nil
}

type RetryOptions struct {
	// Retries is the number of times to retry the request. 0 means no retries.
	Retries int

	// RetryAboveCode is the status code above which the request should be retried.
	RetryAboveCode int
	RetryCodes     []int
}

func NewDefaultRetryOptions() RetryOptions {
	return RetryOptions{
		Retries:        0,   // = one try, no retries
		RetryAboveCode: 1,   // any - doesn't matter
		RetryCodes:     nil, // none - doesn't matter
	}
}

func (r *RetryOptions) complete(opts ...RetryOptions) {
	for _, opt := range opts {
		if opt.Retries > 0 {
			r.Retries = opt.Retries
		}
		if opt.RetryAboveCode > 0 {
			r.RetryAboveCode = opt.RetryAboveCode
		}
		for _, code := range opt.RetryCodes {
			if !slices.Contains(r.RetryCodes, code) {
				r.RetryCodes = append(r.RetryCodes, code)
			}
		}
	}
}

func (r *RetryOptions) canRetry(statusCode int) bool {
	if r.RetryAboveCode > 0 && statusCode > r.RetryAboveCode {
		return true
	}
	return slices.Contains(r.RetryCodes, statusCode)
}

func (c *Client) sendRequest(req *http.Request, v Response, retryOpts ...RetryOptions) error {
	req.Header.Set("Accept", "application/json")

	// Default Options
	options := NewDefaultRetryOptions()
	options.complete(retryOpts...)

	// Check whether Content-Type is already set, Upload Files API requires
	// Content-Type == multipart/form-data
	contentType := req.Header.Get("Content-Type")
	if contentType == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	const baseDelay = time.Millisecond * 200
	var (
		resp     *http.Response
		err      error
		failures []string
	)

	// Save the original request body
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return fmt.Errorf("failed to read request body: %v", err)
		}
	}

	for i := 0; i <= options.Retries; i++ {

		// Reset body to the original request body
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		resp, err = c.config.HTTPClient.Do(req)
		if err == nil && !isFailureStatusCode(resp) {
			defer resp.Body.Close()
			if v != nil {
				v.SetHeader(resp.Header)
			}
			return decodeResponse(resp.Body, v)
		}

		// handle connection errors
		if err != nil {
			failures = append(failures, fmt.Sprintf("#%d/%d failed to send request: %v", i+1, options.Retries+1, err))
			continue
		}

		// handle status codes
		failures = append(failures, fmt.Sprintf("#%d/%d error response received: %v", i+1, options.Retries+1, c.handleErrorResp(resp)))

		// exit on non-retriable status codes
		if !options.canRetry(resp.StatusCode) {
			failures = append(failures, fmt.Sprintf("exiting due to non-retriable error in try #%d/%d: %d %s", i+1, options.Retries+1, resp.StatusCode, resp.Status))
			slog.Error("sendRequest failed due to non-retriable statuscode", "code", resp.StatusCode, "status", resp.Status, "tries", i+1, "maxTries", options.Retries+1, "failures", strings.Join(failures, "; "))
			return fmt.Errorf("request failed on non-retriable status-code: %d %s", resp.StatusCode, resp.Status)
		}

		// exponential backoff
		delay := baseDelay * time.Duration(1<<i)
		jitter := time.Duration(rand.Int63n(int64(baseDelay)))
		select {
		case <-req.Context().Done():
			slog.Error("sendRequest failed due to canceled context", "tries", i+1, "maxTries", options.Retries+1, "failures", strings.Join(failures, "; "))
			return fmt.Errorf("request failed due to canceled context: %v", req.Context().Err())
		case <-time.After(delay + jitter):
		}
	}

	slog.Error("sendRequest failed after exceeding retry limit", "tries", options.Retries+1, "failures", strings.Join(failures, "; "))
	return fmt.Errorf("request exceeded retry limits: %s", failures[len(failures)-1])
}

func sendRequestStream[T streamable](client *Client, req *http.Request, retryOpts ...RetryOptions) (*streamReader[T], error) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Connection", "keep-alive")

	// Default Retry Options
	options := NewDefaultRetryOptions()
	options.complete(retryOpts...)

	const baseDelay = time.Millisecond * 200
	var (
		err      error
		failures []string
	)

	// Save the original request body
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, err = io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read request body: %v", err)
		}
	}

	for i := 0; i <= options.Retries; i++ {

		// Reset body to the original request body
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		resp, err := client.config.HTTPClient.Do(req) //nolint:bodyclose // body is closed in stream.Close()
		if err == nil && !isFailureStatusCode(resp) {
			// we're good!
			return &streamReader[T]{
				emptyMessagesLimit: client.config.EmptyMessagesLimit,
				reader:             bufio.NewReader(resp.Body),
				response:           resp,
				errBuffer:          &bytes.Buffer{},
				httpHeader:         httpHeader(resp.Header),
			}, nil
		}

		if err != nil {
			failures = append(failures, fmt.Sprintf("#%d/%d failed to send request: %v", i+1, options.Retries+1, err))
			continue
		}

		// handle status codes
		failures = append(failures, fmt.Sprintf("#%d/%d error response received: %v", i+1, options.Retries+1, client.handleErrorResp(resp)))

		// exit on non-retriable status codes
		if !options.canRetry(resp.StatusCode) {
			failures = append(failures, fmt.Sprintf("exiting due to non-retriable error in try #%d/%d: %d %s", i+1, options.Retries+1, resp.StatusCode, resp.Status))
			slog.Error("sendRequestStream failed due to non-retriable statuscode", "code", resp.StatusCode, "status", resp.Status, "tries", i+1, "maxTries", options.Retries+1, "failures", strings.Join(failures, "; "))
			return nil, fmt.Errorf("request failed on non-retriable status-code: %d %s", resp.StatusCode, resp.Status)
		}

		// exponential backoff
		delay := baseDelay * time.Duration(1<<i)
		jitter := time.Duration(rand.Int63n(int64(baseDelay)))
		select {
		case <-req.Context().Done():
			failures = append(failures, fmt.Sprintf("exiting due to canceled context after try #%d/%d: %v", i+1, options.Retries+1, req.Context().Err()))
			slog.Error("sendRequestStream failed due to canceled context", "tries", i+1, "maxTries", options.Retries+1, "failures", strings.Join(failures, "; "))
			return nil, fmt.Errorf("request failed due to canceled context: %v", req.Context().Err())
		case <-time.After(delay + jitter):
		}
	}

	slog.Error("sendRequestStream failed after exceeding retry limit", "tries", options.Retries+1, "failures", strings.Join(failures, "; "))
	return nil, fmt.Errorf("request exceeded retry limits: %s", failures[len(failures)-1])
}

func (c *Client) setCommonHeaders(req *http.Request) {
	// https://learn.microsoft.com/en-us/azure/cognitive-services/openai/reference#authentication
	// Azure API Key authentication
	if c.config.APIType == APITypeAzure {
		req.Header.Set(AzureAPIKeyHeader, c.config.authToken)
	} else if c.config.authToken != "" {
		// OpenAI or Azure AD authentication
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", c.config.authToken))
	}
	if c.config.OrgID != "" {
		req.Header.Set("OpenAI-Organization", c.config.OrgID)
	}
}

func isFailureStatusCode(resp *http.Response) bool {
	return resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusBadRequest
}

func decodeResponse(body io.Reader, v any) error {
	if v == nil {
		return nil
	}

	switch o := v.(type) {
	case *string:
		return decodeString(body, o)
	default:
		return json.NewDecoder(body).Decode(v)
	}
}

func decodeString(body io.Reader, output *string) error {
	b, err := io.ReadAll(body)
	if err != nil {
		return err
	}
	*output = string(b)
	return nil
}

// fullURL returns full URL for request.
// args[0] is model name, if API type is Azure, model name is required to get deployment name.
func (c *Client) fullURL(suffix string, args ...any) string {
	// /openai/deployments/{model}/chat/completions?api-version={api_version}
	if c.config.APIType == APITypeAzure || c.config.APIType == APITypeAzureAD {
		baseURL := c.config.BaseURL
		baseURL = strings.TrimRight(baseURL, "/")
		// if suffix is /models change to {endpoint}/openai/models?api-version=2022-12-01
		// https://learn.microsoft.com/en-us/rest/api/cognitiveservices/azureopenaistable/models/list?tabs=HTTP
		if containsSubstr([]string{"/models", "/assistants", "/threads", "/files"}, suffix) {
			return fmt.Sprintf("%s/%s%s?api-version=%s", baseURL, azureAPIPrefix, suffix, c.config.APIVersion)
		}
		azureDeploymentName := "UNKNOWN"
		if len(args) > 0 {
			model, ok := args[0].(string)
			if ok {
				azureDeploymentName = c.config.GetAzureDeploymentByModel(model)
			}
		}
		return fmt.Sprintf("%s/%s/%s/%s%s?api-version=%s",
			baseURL, azureAPIPrefix, azureDeploymentsPrefix,
			azureDeploymentName, suffix, c.config.APIVersion,
		)
	}

	return fmt.Sprintf("%s%s", c.config.BaseURL, suffix)
}

func (c *Client) handleErrorResp(resp *http.Response) error {
	data, _ := io.ReadAll(resp.Body)
	var errRes ErrorResponse
	err := json.NewDecoder(bytes.NewBuffer(data)).Decode(&errRes)
	if err == nil && errRes.Error != nil && errRes.Error.Message != "" {
		errRes.Error.HTTPStatusCode = resp.StatusCode
		return errRes.Error
	}

	return &RequestError{
		HTTPStatusCode: resp.StatusCode,
		Err:            errors.New(string(data)),
	}
}

func containsSubstr(s []string, e string) bool {
	for _, v := range s {
		if strings.Contains(e, v) {
			return true
		}
	}
	return false
}
