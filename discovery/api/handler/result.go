package handler

import (
	"service_discovery/errors"
	"service_discovery/model"
	"time"

	"github.com/gin-gonic/gin"
)

var (
	rg *model.Registry
)

const (
	_pollWaitSecond = 30 * time.Second
)

const (
	contextErrCode = "context/err/code"
)

type resp struct {
	Code int         `json:"code"`
	Data interface{} `json:"data"`
}

func result(c *gin.Context, data interface{}, err error) {
	ee := errors.Code(err)
	c.Set(contextErrCode, ee.Code())
	c.JSON(200, resp{
		Code: ee.Code(),
		Data: data,
	})
}
