package httputil

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	rhttp "net/http"
	"net/url"
	"strings"
	rtime "time"
)

var (
	_minRead int64 = 16 * 1024 // 16kb
)

// ClientConfig is http client conf.
type ClientConfig struct {
	Dial      rtime.Duration
	KeepAlive rtime.Duration
}

// Client is http client.
type Client struct {
	client    *rhttp.Client
	transport rhttp.RoundTripper
}

// NewClient new a http client.
func NewClient(c *ClientConfig) *Client {
	client := new(Client)
	dialer := &net.Dialer{
		Timeout:   rtime.Duration(c.Dial),
		KeepAlive: rtime.Duration(c.KeepAlive),
	}
	client.transport = &rhttp.Transport{
		DialContext:     dialer.DialContext,
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client.client = &rhttp.Client{
		Transport: client.transport,
	}
	return client
}

// NewRequest new http request with method, uri, ip, values and headers.
func (client *Client) NewRequest(method, uri, realIP string, params url.Values) (req *rhttp.Request, err error) {
	if method == rhttp.MethodGet {
		req, err = rhttp.NewRequest(rhttp.MethodGet, uri+"?"+params.Encode(), nil)
	} else {
		req, err = rhttp.NewRequest(rhttp.MethodPost, uri, strings.NewReader(params.Encode()))
	}
	if err != nil {
		return
	}
	const (
		_contentType = "Content-Type"
		_urlencoded  = "application/x-www-form-urlencoded"
	)
	if method == rhttp.MethodPost {
		req.Header.Set(_contentType, _urlencoded)
	}
	return
}

// Get issues a GET to the specified URL.
func (client *Client) Get(c context.Context, uri, ip string, params url.Values, res interface{}) (err error) {
	req, err := client.NewRequest(rhttp.MethodGet, uri, ip, params)
	if err != nil {
		return
	}
	return client.Do(c, req, res)
}

// Post issues a Post to the specified URL.
func (client *Client) Post(c context.Context, uri, ip string, params url.Values, res interface{}) (err error) {
	req, err := client.NewRequest(rhttp.MethodPost, uri, ip, params)
	if err != nil {
		return
	}
	return client.Do(c, req, res)
}

// Raw sends an HTTP request and returns bytes response
func (client *Client) Raw(c context.Context, req *rhttp.Request, v ...string) (bs []byte, err error) {
	resp, err := client.client.Do(req.WithContext(c))
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= rhttp.StatusBadRequest {
		return
	}
	bs, err = readAll(resp.Body, _minRead)
	return
}

// SetTransport set client transport
func (client *Client) SetTransport(t rhttp.RoundTripper) {
	client.transport = t
	client.client.Transport = t
}

// Do sends an HTTP request and returns an HTTP json response.
func (client *Client) Do(c context.Context, req *rhttp.Request, res interface{}, v ...string) (err error) {
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
