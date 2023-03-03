package xhttp

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	_minRead int64 = 16 * 1024 // 16kb
)

// ClientConfig is http client conf.
//go:generate easytags $GOFILE json
type ClientConfig struct {
	Dial      time.Duration `json:"dial"`
	KeepAlive time.Duration `json:"keep_alive"`
}

// Client is http client.
type Client struct {
	client    *http.Client
	transport http.RoundTripper
}

// NewClient new a http client.
func NewClient(c *ClientConfig) *Client {
	client := new(Client)
	dialer := &net.Dialer{
		Timeout:   time.Duration(c.Dial),
		KeepAlive: time.Duration(c.KeepAlive),
	}
	client.transport = &http.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client.client = &http.Client{
		Transport: client.transport,
	}
	return client
}

// NewRequest new http request with method, uri, ip, values and headers.
func (client *Client) NewRequest(method, uri, realIP string, params url.Values) (req *http.Request, err error) {
	if method == http.MethodGet {
		req, err = http.NewRequest(http.MethodGet, uri+"?"+params.Encode(), nil)
	} else {
		req, err = http.NewRequest(http.MethodPost, uri, strings.NewReader(params.Encode()))
	}
	if err != nil {
		return
	}
	const (
		_contentType = "Content-Type"
		_urlencoded  = "application/x-www-form-urlencoded"
	)
	if method == http.MethodPost {
		req.Header.Set(_contentType, _urlencoded)
	}
	return
}

// Get issues a GET to the specified URL.
func (client *Client) Get(c context.Context, uri, ip string, params url.Values, res interface{}) (err error) {
	req, err := client.NewRequest(http.MethodGet, uri, ip, params)
	if err != nil {
		return
	}
	return client.Do(c, req, res)
}

// Post issues a Post to the specified URL.
func (client *Client) Post(c context.Context, uri, ip string, params url.Values, res interface{}) (err error) {
	req, err := client.NewRequest(http.MethodPost, uri, ip, params)
	if err != nil {
		return
	}
	return client.Do(c, req, res)
}

// Raw sends an HTTP request and returns bytes response
func (client *Client) Raw(c context.Context, req *http.Request, v ...string) (bs []byte, err error) {
	resp, err := client.client.Do(req.WithContext(c))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return
	}
	bs, err = readAll(resp.Body, _minRead)
	return
}

// SetTransport set client transport
func (client *Client) SetTransport(t http.RoundTripper) {
	client.transport = t
	client.client.Transport = t
}

// Do sends an HTTP request and returns an HTTP json response.
func (client *Client) Do(c context.Context, req *http.Request, res interface{}, v ...string) (err error) {
	var bs []byte
	if bs, err = client.Raw(c, req, v...); err != nil {
		return
	}
	if res != nil {
		err = json.Unmarshal(bs, res)
	}
	return
}

// readAll reads from r until an error or EOF and returns the data it read
// from the internal buffer allocated with a specified capacity.
func readAll(r io.Reader, capacity int64) (b []byte, err error) {
	buf := bytes.NewBuffer(make([]byte, 0, capacity))
	// If the buffer overflows, we will get bytes.ErrTooLarge.
	// Return that as an error. Any other panic remains.
	defer func() {
		e := recover()
		if e == nil {
			return
		}
		if panicErr, ok := e.(error); ok && panicErr == bytes.ErrTooLarge {
			err = panicErr
		} else {
			panic(e)
		}
	}()
	_, err = buf.ReadFrom(r)
	return buf.Bytes(), err
}
