package handler

import (
	"log"
	"net/http"
	"service_discovery/global"
	"service_discovery/model"
	"service_discovery/pkg/errcode"

	"github.com/gin-gonic/gin"
)

func FetchHandler(c *gin.Context) {
	log.Println("request api/fetch...")
	var req model.ArgFetch
	if e := c.ShouldBindJSON(&req); e != nil {
		err := errcode.ParamError
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"message": err.Error(),
		})
		return
	}

	//fetch
	fetchData, err := global.Discovery.Registry.Fetch(req.Zone, req.Env, req.AppID, req.Status, 0)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code":    err.Code(),
			"data":    "",
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"data":    fetchData,
		"message": "",
	})
}
