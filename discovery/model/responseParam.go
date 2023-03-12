package model

import (
	"encoding/json"
)

type Response struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

type FetchData struct {
	Instances       []*Instance `json:"instances"`
	LatestTimestamp int64       `json:"latest_timestamp"`
}

type ResponseFetch struct {
	Response
	Data FetchData `json:"data"`
}

// api nodes
type ArgNodes struct {
	Zone string `form:"zone"`
	Env  string `form:"env"`
}

// api poll
type ArgPoll struct {
	Zone            string `form:"zone"`
	Env             string `form:"env" validate:"required"`
	AppID           string `form:"appid" validate:"required"`
	Hostname        string `form:"hostname" validate:"required"`
	LatestTimestamp int64  `form:"latest_timestamp"`
}

// api Polls
type ArgPolls struct {
	Zone            string   `form:"zone"`
	Env             string   `form:"env" validate:"required"`
	AppID           []string `form:"appid" validate:"gt=0"`
	Hostname        string   `form:"hostname" validate:"required"`
	LatestTimestamp []int64  `form:"latest_timestamp"`
}
