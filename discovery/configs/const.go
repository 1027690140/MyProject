package configs

import (
	"time"
)

const (
	NodeStatusUp = iota + 1 //InstanceStatusUP
	NodeStatusDown
)

const (
	StatusOK = 200
)

const (
	StatusReceive = iota + 1
	StatusNotReceive
)

const (
	RegisterURL = "/api/register"
	CancelURL   = "/api/cancel"
	RenewURL    = "/api/renew"
	FetchAllURL = "/api/fetchall"
)

const (
	DiscoveryAppID = "my.discovery"
)

const (
	RenewInterval               = 30 * time.Second   //client heart beat interval
	CheckEvictInterval          = 60 * time.Second   //evict task interval
	SelfProtectThreshold        = 0.85               //self protect threshold
	ResetGuardNeedCountInterval = 15 * time.Minute   //ticker reset guard need count
	InstanceExpireDuration      = 90 * time.Second   //instance's renewTimestamp after this will be canceled
	InstanceMaxExpireDuration   = 3600 * time.Second //instance's renewTimestamp after this will be canceled
	ProtectTimeInterval         = 60 * time.Second   //two renew cycle
	NodePerceptionInterval      = 5 * time.Second    //nodesprotect
)

// client Transport 的参数
const (
	EnableKeepAlives    = true             //开启长连接
	DisableKeepAlives   = false            //不开启长连接
	MaxIdleConns        = 30               //最大连接数
	MaxIdleConnsPerHost = 10               //每个host可以发出的最大连接个数
	IdleConnTimeout     = 60 * time.Second //多长时间未使用自动关闭连接
)
