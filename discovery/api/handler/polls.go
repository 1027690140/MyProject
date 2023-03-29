package handler

import (
	"fmt"
	"service_discovery/errors"
	"service_discovery/model"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sanity-io/litter"
)

func Poll(c *gin.Context) {
	arg := new(model.ArgPolls)
	if err := c.Bind(arg); err != nil {
		result(c, nil, errors.ParamsErr)
		return
	}
	fmt.Println("-------------------> poll  >>")
	litter.Dump(arg)
	connOption := new(model.ConnOption)
	ch, news, err := rg.Polls(c, arg, connOption)
	if err != nil && err != errors.NotModified {
		result(c, nil, err)
		return
	}

	// wait for instance change
	select {
	case e := <-ch:
		result(c, resp{Data: e[arg.AppID[0]]}, nil)
		if !news {
			rg.DelConns(arg) // broadcast will delete all connections of appid
		}
		return
	case <-time.After(_pollWaitSecond):
		result(c, nil, errors.NotModified)
	case <-c.Done():
	}
	result(c, nil, errors.NotModified)
	rg.DelConns(arg)
}

func Polls(c *gin.Context) {
	arg := new(model.ArgPolls)
	if err := c.Bind(arg); err != nil {
		result(c, nil, errors.ParamsErr)
		return
	}

	// if len(arg.AppID) != len(arg.LatestTimestamp) {
	// 	result(c, nil, errors.ParamsErr)
	// 	return
	// }
	connOption := new(model.ConnOption)
	ch, news, err := rg.Polls(c, arg, connOption)
	if err != nil && err != errors.NotModified {
		result(c, nil, err)
		return
	}
	// wait for instance change
	select {
	case e := <-ch:
		result(c, e, nil)
		if !news {
			rg.DelConns(arg) // broadcast will delete all connections of appid
		}
		return
	case <-time.After(_pollWaitSecond):
	case <-c.Done():
	}
	result(c, nil, errors.NotModified)
	rg.DelConns(arg)
}
