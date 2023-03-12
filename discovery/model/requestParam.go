package model

// api register
type ArgRequest struct {
	// TODO
	Region string `form:"region"`

	Zone            string   `form:"zone" validate:"required"`
	Env             string   `form:"env" validate:"required"`
	AppID           string   `form:"AppID" validate:"required"`
	Hostname        string   `form:"hostname" validate:"required"`
	Status          uint32   `form:"status" validate:"required"`
	Addrs           []string `form:"addrs" validate:"gt=0"`
	Version         string   `form:"version"`
	Metadata        string   `form:"metadata"`
	Replication     bool     `form:"replication"`
	LatestTimestamp int64    `form:"latest_timestamp"`
	DirtyTimestamp  int64    `form:"dirty_timestamp"`
}

// api renew heart beat
type ArgRenew struct {
	Zone           string `form:"zone" validate:"required"`
	Env            string `form:"env" validate:"required"`
	AppID          string `form:"AppID" validate:"required"`
	Hostname       string `form:"hostname" validate:"required"`
	DirtyTimestamp int64  `form:"dirty_timestamp"` //other node send
	Replication    bool   `form:"replication"`     //other node send
}

// api cancel
type ArgCancel struct {
	Zone            string `form:"zone" validate:"required"`
	Env             string `form:"env" validate:"required"`
	AppID           string `form:"AppID" validate:"required"`
	Hostname        string `form:"hostname" validate:"required"`
	Replication     bool   `form:"replication"`
	LatestTimestamp int64  `form:"latest_timestamp"`
}

// api fetch
type ArgFetch struct {
	Zone   string `form:"zone"`
	Env    string `from:"env"`
	AppID  string `form:"AppID"`
	Status uint32 `form:"status"`
}

// api fetch more
type ArgFetchs struct {
	Zone   string   `form:"zone"`
	Env    string   `form:"env"`
	AppID  []string `form:"AppID"`
	Status uint32   `form:"status"`
}

// ArgSet define set param.
type ArgSet struct {
	Zone         string   `form:"zone" validate:"required"`
	Env          string   `form:"env" validate:"required"`
	AppID        string   `form:"AppID" validate:"required"`
	Hostname     []string `form:"hostname" validate:"gte=0"`
	Status       []uint32 `form:"status" validate:"gte=0"`
	Metadata     []string `form:"metadata" validate:"gte=0"`
	Replication  bool     `form:"replication"`
	SetTimestamp int64    `form:"set_timestamp"`
}
